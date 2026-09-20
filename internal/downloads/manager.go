package downloads

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/cbz"
	"github.com/Asion001/mangarr/internal/comicinfo"
	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/fsutil"
	"github.com/Asion001/mangarr/internal/history"
	"github.com/Asion001/mangarr/internal/imagecheck"
	"github.com/Asion001/mangarr/internal/library"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/progress"
	"github.com/Asion001/mangarr/internal/quiet"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/sourcegov"
	"github.com/Asion001/mangarr/internal/worktasks"
)

// PageFile is a validated page on disk.
type PageFile struct {
	Name   string // archive name, e.g. 0001.jpg
	Path   string
	Format string
	Width  int
	Height int
}

// Processor transforms pages before import (e.g. upscaling). applied=false
// means pages were left untouched.
// ProcessResult is the outcome of the processing stage.
type ProcessResult struct {
	Pages   []PageFile
	Changed bool // pages differ from the input
	// ProcessedPages counts pages whose stored image changed. It deliberately
	// excludes pages inspected by a no-op run so speed statistics stay honest.
	ProcessedPages int
	Upscaled       bool
	UpscaleModel   string
	Encoded        int // pages re-encoded
	Encoder        string
	// Seconds is how long processing took (set by the manager).
	Seconds float64
}

// Processor upscales and/or re-encodes pages according to a profile.
type Processor interface {
	Process(ctx context.Context, cfg model.ProfileConfig, pages []PageFile, workDir string) (ProcessResult, error)
}

// permanentError marks failures that should blocklist the release.
type permanentError struct{ error }

func permanent(err error) error { return permanentError{err} }

// infraError marks failures of our own infrastructure (engine down, disk full)
// that must never blocklist a release.
type infraError struct{ error }

type Manager struct {
	db       *db.DB
	bus      *events.Bus
	mods     *modules.Manager
	settings *settings.Store
	lib      *library.Library
	queue    *Queue
	searcher *Searcher
	log      *slog.Logger
	dataDir  string

	Processor Processor
	// Live has the progress of running jobs.
	Live *Live
	// Gov (optional) paces chapters per catalog and defers throttled catalogs.
	Gov *sourcegov.Governor
	// Tasks (optional) is the ledger of work handed to workers. Jobs with a
	// task still open belong to a worker, not to this process.
	Tasks *worktasks.Ledger

	mu          sync.Mutex
	running     map[int64]context.CancelFunc
	runningKind map[int64]string
	runningSrc  map[string]int
	lastMaint   time.Time
	wasBusy     bool
	held        bool
	progressMu  sync.Mutex
	lastPersist map[int64]time.Time
}

func NewManager(d *db.DB, bus *events.Bus, mods *modules.Manager, st *settings.Store, lib *library.Library, q *Queue, s *Searcher, log *slog.Logger, dataDir string) *Manager {
	return &Manager{db: d, bus: bus, mods: mods, settings: st, lib: lib, queue: q, searcher: s, log: log, dataDir: dataDir,
		running: map[int64]context.CancelFunc{}, runningKind: map[int64]string{}, runningSrc: map[string]int{}, lastPersist: map[int64]time.Time{},
		Live: NewLive(bus)}
}

// Start recovers interrupted jobs and starts the scheduling loop. Jobs a
// worker still holds are left alone: the restart was ours, not theirs, and
// their pages are still arriving in staging.
func (m *Manager) Start(ctx context.Context) error {
	live, err := m.openJobs(ctx)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	q := m.db.NewUpdate().Model((*model.DownloadJob)(nil)).
		Set("status = ?", model.JobQueued).Set("not_before = ?", now).Set("updated_at = ?", now).
		Where("status IN (?)", bun.In([]string{model.JobDownloading, model.JobProcessing, model.JobImporting}))
	if len(live) > 0 {
		q = q.Where("id NOT IN (?)", bun.In(keys(live)))
	}
	if _, err := q.Exec(ctx); err != nil {
		return err
	}
	m.pruneStaging(ctx, live)
	go m.loop(ctx)
	return nil
}

// openJobs is the jobs a worker is still busy with.
func (m *Manager) openJobs(ctx context.Context) (map[int64]bool, error) {
	if m.Tasks == nil {
		return nil, nil
	}
	return m.Tasks.OpenJobs(ctx)
}

func keys(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// pruneStaging drops the working directories of jobs nobody is doing any
// more, and keeps the ones a worker is still uploading into.
func (m *Manager) pruneStaging(ctx context.Context, live map[int64]bool) {
	staging := filepath.Join(m.dataDir, "staging")
	if len(live) == 0 {
		_ = os.RemoveAll(staging)
		return
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		return
	}
	for _, e := range entries {
		id, err := strconv.ParseInt(strings.TrimPrefix(e.Name(), "job-"), 10, 64)
		if err != nil || !live[id] {
			_ = os.RemoveAll(filepath.Join(staging, e.Name()))
		}
	}
}

// Hold stops starting jobs (true) or starts again (false).
func (m *Manager) Hold(on bool) {
	m.mu.Lock()
	m.held = on
	m.mu.Unlock()
	m.queue.signal()
}

// CancelAll stops every running job; they're queued again on next start.
func (m *Manager) CancelAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.running {
		c()
	}
}

// Running is the number of jobs running now.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.running)
}

// Cancel stops a running job (used when the user removes it from the queue),
// here and on whichever worker is doing it.
func (m *Manager) Cancel(jobID int64) {
	m.mu.Lock()
	if c, ok := m.running[jobID]; ok {
		c()
	}
	m.mu.Unlock()
	if m.Tasks != nil {
		if err := m.Tasks.Cancel(context.Background(), jobID); err != nil {
			m.log.Warn("could not cancel a job's worker tasks", "job", jobID, "err", err)
		}
	}
}

func (m *Manager) loop(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		m.dispatch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.queue.wake:
		}
	}
}

type queuedJob struct {
	model.DownloadJob
	ModuleID int64  `bun:"module_id"`
	SourceID string `bun:"source_id"`
}

// srcKey identifies the catalog of a job for per-source limits.
func srcKey(moduleID int64, sourceID string) string {
	if sourceID == "" {
		return ""
	}
	return strconv.FormatInt(moduleID, 10) + ":" + sourceID
}

func (m *Manager) dispatch(ctx context.Context) {
	dl, _ := m.settings.Downloads(ctx)
	if dl.MaxConcurrent <= 0 {
		dl.MaxConcurrent = 1
	}
	if dl.MaxPerSource <= 0 {
		dl.MaxPerSource = 1
	}
	now := time.Now()
	if qs, _ := m.settings.QueueState(ctx); qs.Active(now) {
		return // the whole queue is paused
	}
	sched, _ := m.settings.Schedule(ctx)
	quietNow := quiet.Evaluate(sched, now)
	var jobs []queuedJob
	err := m.db.NewSelect().TableExpr("download_jobs AS j").ColumnExpr("j.*, COALESCE(ss.module_id, 0) AS module_id, COALESCE(ss.source_id, '') AS source_id").
		Join("LEFT JOIN chapter_releases AS r ON r.id = j.release_id").
		Join("LEFT JOIN series_sources AS ss ON ss.id = r.series_source_id").
		Where("j.status = ? AND j.not_before <= ?", model.JobQueued, time.Now().UTC()).
		OrderExpr("j.priority DESC, j.id").Limit(100).Scan(ctx, &jobs)
	if err != nil {
		if ctx.Err() == nil {
			m.log.Error("download queue", "err", err)
		}
		return
	}
	// jobs a worker holds are not ours to run
	onWorkers, err := m.openJobs(ctx)
	if err != nil {
		m.log.Warn("could not read the worker tasks", "err", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.held {
		return
	}
	busy := len(m.running) > 0
	for _, j := range jobs {
		if len(m.running) >= dl.MaxConcurrent {
			break
		}
		if _, ok := m.running[j.ID]; ok {
			continue
		}
		if onWorkers[j.ID] {
			continue
		}
		if (j.Kind == model.JobKindDownload && quietNow.PauseDownloads) || (j.Kind == model.JobKindReprocess && quietNow.PauseProcessing) {
			continue // quiet hours
		}
		if j.Kind == model.JobKindReprocess && m.runningOfKind(model.JobKindReprocess) >= MaxReprocess {
			continue // processing is GPU/CPU heavy: one at a time
		}
		src := srcKey(j.ModuleID, j.SourceID)
		if j.Kind == model.JobKindReprocess {
			src = "" // reworks the file on disk: never talks to the source
		}
		if src != "" && m.runningSrc[src] >= dl.MaxPerSource {
			continue
		}
		if m.Gov != nil && src != "" {
			if until, _ := m.Gov.Cooldown(sourcegov.Key{ModuleID: j.ModuleID, SourceID: j.SourceID}); !until.IsZero() {
				continue // throttled catalog: wait for the cooldown
			}
		}
		jctx, cancel := context.WithCancel(ctx)
		m.running[j.ID] = cancel
		m.runningKind[j.ID] = j.Kind
		m.runningSrc[src]++
		busy = true
		go func(job model.DownloadJob, src string) {
			defer func() {
				cancel()
				m.mu.Lock()
				delete(m.running, job.ID)
				delete(m.runningKind, job.ID)
				m.runningSrc[src]--
				m.mu.Unlock()
				m.progressMu.Lock()
				delete(m.lastPersist, job.ID)
				m.progressMu.Unlock()
				m.queue.signal()
			}()
			// a worker with the right role takes downloads off this machine;
			// anything it can't have runs here
			handed, err := m.offload(jctx, job)
			if err != nil {
				m.log.Warn("could not hand a chapter to the workers", "job", job.ID, "err", err)
			}
			if !handed {
				m.run(jctx, job)
			}
		}(j.DownloadJob, src)
	}
	// Queue drained: run module housekeeping (e.g. clear engine page caches).
	if !busy && m.wasBusy && time.Since(m.lastMaint) > 10*time.Minute {
		m.lastMaint = time.Now()
		go m.maintain(context.WithoutCancel(ctx))
	}
	m.wasBusy = busy
}

func (m *Manager) maintain(ctx context.Context) {
	for _, mt := range modules.ActiveAs[source.Maintainer](m.mods, modules.KindSource) {
		if err := mt.Instance.Maintain(ctx); err != nil {
			m.log.Debug("source maintenance", "module", mt.Def.Name, "err", err)
		}
	}
}

// jobCtx bundles everything loaded for a job.
type jobCtx struct {
	job     *model.DownloadJob
	series  model.Series
	profile model.Profile
	chapter model.Chapter
	release *model.ChapterRelease
	link    *model.SeriesSource
	file    *model.ChapterFile
}

func (m *Manager) load(ctx context.Context, job *model.DownloadJob) (*jobCtx, error) {
	jc := &jobCtx{job: job}
	if err := m.db.NewSelect().Model(&jc.chapter).Where("id = ?", job.ChapterID).Scan(ctx); err != nil {
		return nil, err
	}
	if err := m.db.NewSelect().Model(&jc.series).Where("id = ?", job.SeriesID).Scan(ctx); err != nil {
		return nil, err
	}
	if err := m.db.NewSelect().Model(&jc.profile).Where("id = ?", jc.series.ProfileID).Scan(ctx); err != nil {
		return nil, err
	}
	if jc.chapter.FileID != nil {
		var f model.ChapterFile
		if err := m.db.NewSelect().Model(&f).Where("id = ?", *jc.chapter.FileID).Scan(ctx); err == nil {
			jc.file = &f
		}
	}
	if job.ReleaseID != nil {
		var r model.ChapterRelease
		if err := m.db.NewSelect().Model(&r).Where("id = ?", *job.ReleaseID).Scan(ctx); err == nil {
			jc.release = &r
			var ss model.SeriesSource
			if err := m.db.NewSelect().Model(&ss).Where("id = ?", r.SeriesSourceID).Scan(ctx); err == nil {
				jc.link = &ss
			}
		}
	}
	return jc, nil
}

func (m *Manager) setStatus(ctx context.Context, job *model.DownloadJob, status string, chapterState string) {
	now := time.Now().UTC()
	job.Status, job.UpdatedAt = status, now
	cols := []string{"status", "updated_at"}
	if status == model.JobDownloading && job.StartedAt == nil {
		job.StartedAt = &now
		cols = append(cols, "started_at")
	}
	// compare-and-set: a job paused meanwhile keeps its paused status
	res, err := m.db.NewUpdate().Model(job).Column(cols...).WherePK().Where("status <> ?", model.JobPaused).Exec(ctx)
	if err != nil {
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	if chapterState != "" && !job.IsUpgrade && job.Kind == model.JobKindDownload {
		_, _ = m.db.NewUpdate().Model((*model.Chapter)(nil)).Set("state = ?", chapterState).Set("updated_at = ?", now).Where("id = ?", job.ChapterID).Exec(ctx)
		m.bus.Changed("chapter", "updated", job.ChapterID)
	}
	m.bus.Changed("queue", "updated", job.ID)
}

// progress records how far a download is, for the queue and the live
// stream. bytesIn is what has been pulled from the site so far (0 when the
// caller doesn't count it).
func (m *Manager) progress(job *model.DownloadJob, done, total int, bytesIn int64) {
	m.Live.Update(job.ID, progress.Event{Stage: progress.StageDownload, Done: done, Total: total, BytesIn: bytesIn})
	job.PagesDone, job.PagesTotal = done, total
	if total > 0 {
		job.Progress = done * 100 / total
	}
	m.progressMu.Lock()
	last := m.lastPersist[job.ID]
	persist := time.Since(last) > 2*time.Second || done == total
	if persist {
		m.lastPersist[job.ID] = time.Now()
	}
	m.progressMu.Unlock()
	if persist {
		_, _ = m.db.NewUpdate().Model(job).Column("pages_done", "pages_total", "progress").WherePK().Exec(context.Background())
		m.bus.Changed("queue", "updated", job.ID)
	}
}

// claim atomically moves a queued job to "downloading" so a stale dispatch
// snapshot can never run the same job twice.
func (m *Manager) claim(ctx context.Context, job *model.DownloadJob) bool {
	now := time.Now().UTC()
	res, err := m.db.NewUpdate().Model((*model.DownloadJob)(nil)).
		Set("status = ?", model.JobDownloading).Set("updated_at = ?", now).
		Where("id = ? AND status = ?", job.ID, model.JobQueued).Exec(ctx)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n == 1
}

func (m *Manager) run(ctx context.Context, job model.DownloadJob) {
	if !m.claim(ctx, &job) {
		return
	}
	m.Live.Start(job.ID, job.Kind)
	defer m.Live.Finish(job.ID)
	ctx = progress.With(ctx, m.Live.Reporter(job.ID))
	ctx = worktasks.WithJob(ctx, job.ID) // work started deeper in belongs to this job
	log := m.log.With("job", job.ID, "chapterId", job.ChapterID)
	jc, err := m.load(ctx, &job)
	if err != nil {
		log.Error("load job", "err", err)
		m.fail(ctx, &job, nil, err)
		return
	}
	workDir := m.workDir(job.ID)
	defer os.RemoveAll(workDir)
	if err := os.MkdirAll(workDir, 0o775); err != nil {
		m.fail(ctx, &job, jc, infraError{err})
		return
	}
	var pages []PageFile
	switch job.Kind {
	case model.JobKindReprocess:
		pages, err = m.extractExisting(jc, workDir)
	default:
		if jc.release == nil || jc.link == nil {
			cand, upgrade, cerr := m.searcher.NextCandidate(ctx, jc.series.ID, jc.chapter.ID)
			if cerr != nil || cand == nil {
				m.fail(ctx, &job, jc, permanent(errors.New("no downloadable release left for this chapter")))
				return
			}
			rel, link := cand.Release, cand.Source
			jc.release, jc.link, job.ReleaseID, job.IsUpgrade = &rel, &link, &rel.ID, upgrade
			_, _ = m.db.NewUpdate().Model(&job).Column("release_id", "is_upgrade").WherePK().Exec(ctx)
		}
		m.setStatus(ctx, &job, model.JobDownloading, model.ChapterDownloading)
		if m.Gov != nil {
			// random pause between chapters of the same catalog
			if err := m.Gov.Pace(ctx, sourcegov.Key{ModuleID: jc.link.ModuleID, SourceID: jc.link.SourceID}, "chapter"); err != nil {
				m.fail(ctx, &job, jc, err)
				return
			}
		}
		pages, err = m.fetchPages(ctx, jc, workDir)
	}
	if err != nil {
		m.fail(ctx, &job, jc, err)
		return
	}
	m.finish(ctx, job, jc, pages, workDir)
}

// finish is the second half of a download: processing and import. Pages
// reach it either from here or from a worker that uploaded them, and
// everything after this point is the same either way.
func (m *Manager) finish(ctx context.Context, job model.DownloadJob, jc *jobCtx, pages []PageFile, workDir string) {
	log := m.log.With("job", job.ID, "chapterId", job.ChapterID)
	ctx = worktasks.WithJob(ctx, job.ID)
	// processing (upscale / re-encode)
	cfg := jc.profile.Config
	params := cfg.ProcessParams()
	var sizeBefore int64
	for _, p := range pages {
		if st, err := os.Stat(p.Path); err == nil {
			sizeBefore += st.Size()
		}
	}
	proc := ProcessResult{Pages: pages}
	processed := false
	if job.Kind == model.JobKindReprocess && params == "" {
		m.markProcessed(ctx, jc.file, "", 0)
		m.completeUnchanged(ctx, &job, "processing is disabled for this series' profile")
		return
	}
	processingPaused := false
	if job.Kind == model.JobKindDownload {
		sched, _ := m.settings.Schedule(ctx)
		processingPaused = quiet.Evaluate(sched, time.Now()).PauseProcessing
	}
	inline := job.Kind == model.JobKindReprocess || cfg.ProcessTiming == "inline"
	if m.Processor != nil && params != "" && inline && !processingPaused {
		m.setStatus(ctx, &job, model.JobProcessing, model.ChapterProcessing)
		started := time.Now()
		res, perr := m.Processor.Process(ctx, cfg, pages, workDir)
		res.Seconds = time.Since(started).Seconds()
		if res.ProcessedPages == 0 {
			res.Seconds = 0
		}
		var tmp interface{ Temporary() bool }
		switch {
		case perr != nil && ctx.Err() != nil:
			m.fail(ctx, &job, jc, ctx.Err())
			return
		case perr != nil && job.Kind == model.JobKindReprocess && errors.As(perr, &tmp) && tmp.Temporary():
			// the file on disk is fine; retry once the engine is back
			m.markProcessRetry(ctx, jc.file, perr)
			m.fail(ctx, &job, jc, infraError{perr})
			return
		case perr != nil && job.Kind == model.JobKindReprocess:
			m.markProcessFailed(ctx, jc.file, perr)
			m.fail(ctx, &job, jc, permanent(perr))
			return
		case perr != nil:
			log.Warn("processing failed, importing original pages", "err", perr)
			m.bus.Publish(events.Event{Type: events.HealthIssue, SeriesID: jc.series.ID, Payload: events.MessagePayload{
				Title: "Processing failed", Message: fmt.Sprintf("%s ch. %s was imported without processing (it will be retried in the background): %v", jc.series.Title, jc.chapter.NumberKey, perr)}})
		default:
			proc, processed = res, true
		}
		if processed && job.Kind == model.JobKindReprocess && !proc.Changed {
			// nothing to upscale and re-encoding wouldn't save space
			m.markProcessed(ctx, jc.file, params, proc.Seconds)
			m.completeUnchanged(ctx, &job, "nothing to change")
			return
		}
	}
	if !processed {
		params = "" // the background backlog will process it
	}

	m.setStatus(ctx, &job, model.JobImporting, "")
	if err := m.importChapter(ctx, jc, proc, params, sizeBefore); err != nil {
		m.fail(ctx, &job, jc, err)
		return
	}
	log.Info("chapter imported", "series", jc.series.Title, "chapter", jc.chapter.NumberKey, "pages", len(proc.Pages),
		"upscaled", proc.Upscaled, "encoded", proc.Encoded)
}

// markProcessed records that a file was processed with params (no rewrite).
func (m *Manager) markProcessed(ctx context.Context, f *model.ChapterFile, params string, seconds float64) {
	if f == nil {
		return
	}
	now := time.Now().UTC()
	pages := 0
	if seconds > 0 {
		pages = f.PageCount
	}
	_, _ = m.db.NewUpdate().Model((*model.ChapterFile)(nil)).Set("process_params = ?", params).Set("process_state = ?", model.ProcessDone).
		Set("process_seconds = ?", seconds).Set("process_pages = ?", pages).
		Set("process_error = ''").Set("process_attempts = 0").Set("process_retry_at = NULL").Set("processed_at = ?", now).
		Where("id = ?", f.ID).Exec(ctx)
	m.bus.Changed("chapter", "updated", f.ChapterID)
}

// markProcessRetry postpones processing after a temporary failure.
func (m *Manager) markProcessRetry(ctx context.Context, f *model.ChapterFile, err error) {
	if f == nil {
		return
	}
	at := time.Now().UTC().Add(30 * time.Minute)
	_, _ = m.db.NewUpdate().Model((*model.ChapterFile)(nil)).Set("process_error = ?", err.Error()).Set("process_retry_at = ?", at).
		Where("id = ?", f.ID).Exec(ctx)
}

// markProcessFailed counts a failed processing attempt; the backlog retries
// with growing delays and gives up after MaxProcessAttempts.
func (m *Manager) markProcessFailed(ctx context.Context, f *model.ChapterFile, err error) {
	if f == nil {
		return
	}
	delay := time.Duration(1<<min(f.ProcessAttempts, 5)) * time.Hour
	at := time.Now().UTC().Add(delay)
	_, _ = m.db.NewUpdate().Model((*model.ChapterFile)(nil)).Set("process_state = ?", model.ProcessFailed).Set("process_error = ?", err.Error()).
		Set("process_attempts = process_attempts + 1").Set("process_retry_at = ?", at).Where("id = ?", f.ID).Exec(ctx)
	m.bus.Changed("chapter", "updated", f.ChapterID)
}

// MaxProcessAttempts is how often the backlog retries a failing file.
const MaxProcessAttempts = 5

// MaxReprocess is the number of reprocess (upscale/re-encode) jobs run at once.
const MaxReprocess = 1

func (m *Manager) runningOfKind(kind string) int {
	n := 0
	for _, k := range m.runningKind {
		if k == kind {
			n++
		}
	}
	return n
}

// Bulk applies an action to queue entries and returns how many changed.
// Actions: pause, resume, retry, remove, blocklist, top, bottom.
func (m *Manager) Bulk(ctx context.Context, ids []int64, action string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	affected := 0
	exec := func(q *bun.UpdateQuery) error {
		res, err := q.Where("id IN (?)", bun.In(ids)).Exec(ctx)
		if err == nil {
			n, _ := res.RowsAffected()
			affected = int(n)
		}
		return err
	}
	var err error
	switch action {
	case "pause":
		err = exec(m.db.NewUpdate().Model((*model.DownloadJob)(nil)).Set("status = ?", model.JobPaused).Set("updated_at = ?", now).
			Where("status IN (?)", bun.In([]string{model.JobQueued, model.JobDownloading, model.JobProcessing})))
		if err == nil {
			for _, id := range ids {
				m.Cancel(id) // stops running ones; their status stays paused
			}
			// chapters of paused downloads show as queued again
			_, err = m.db.NewUpdate().Model((*model.Chapter)(nil)).Set("state = ?", model.ChapterQueued).Set("updated_at = ?", now).
				Where("id IN (SELECT chapter_id FROM download_jobs WHERE id IN (?) AND status = ? AND kind = ? AND is_upgrade = ?)", bun.In(ids), model.JobPaused, model.JobKindDownload, false).
				Where("state IN (?)", bun.In([]string{model.ChapterDownloading, model.ChapterProcessing})).Exec(ctx)
		}
	case "resume":
		err = exec(m.db.NewUpdate().Model((*model.DownloadJob)(nil)).Set("status = ?", model.JobQueued).Set("not_before = ?", now).Set("updated_at = ?", now).
			Where("status = ?", model.JobPaused))
	case "retry":
		err = exec(m.db.NewUpdate().Model((*model.DownloadJob)(nil)).Set("status = ?", model.JobQueued).Set("error = ''").Set("attempt = 0").
			Set("not_before = ?", now).Set("updated_at = ?", now).Where("status = ?", model.JobFailed))
	case "remove", "blocklist":
		for _, id := range ids {
			m.Cancel(id)
			if e := m.queue.Remove(ctx, id, action == "blocklist"); e == nil {
				affected++
			} else if !errors.Is(e, sql.ErrNoRows) {
				err = e
			}
		}
	case "top", "bottom":
		var edge int
		agg := "MAX(priority)"
		if action == "bottom" {
			agg = "MIN(priority)"
		}
		if e := m.db.NewSelect().Model((*model.DownloadJob)(nil)).ColumnExpr("COALESCE("+agg+", 0)").
			Where("status IN (?)", bun.In(activeStatuses)).Scan(ctx, &edge); e != nil {
			return 0, e
		}
		p := edge + 1
		if action == "bottom" {
			p = edge - 1
		}
		err = exec(m.db.NewUpdate().Model((*model.DownloadJob)(nil)).Set("priority = ?", p).Set("updated_at = ?", now).
			Where("status IN (?)", bun.In(activeStatuses)))
	default:
		return 0, fmt.Errorf("unknown action %q", action)
	}
	m.queue.signal()
	m.bus.Changed("queue", "sync", 0)
	m.bus.Changed("chapter", "updated", 0)
	return affected, err
}

// completeUnchanged finishes a reprocess job that had nothing to do, leaving
// the chapter file untouched.
func (m *Manager) completeUnchanged(ctx context.Context, job *model.DownloadJob, reason string) {
	job.Status, job.Progress, job.Error, job.UpdatedAt = model.JobCompleted, 100, "", time.Now().UTC()
	_, _ = m.db.NewUpdate().Model(job).Column("status", "progress", "error", "updated_at").WherePK().Where("status <> ?", model.JobPaused).Exec(ctx)
	m.log.Info("reprocess skipped", "job", job.ID, "chapter", job.ChapterID, "reason", reason)
	m.bus.Changed("queue", "updated", job.ID)
}

// fetchPages downloads and validates every page into workDir.
func (m *Manager) fetchPages(ctx context.Context, jc *jobCtx, workDir string) ([]PageFile, error) {
	mod, _, err := modules.GetAs[source.Module](m.mods, jc.link.ModuleID)
	if err != nil {
		return nil, infraError{err}
	}
	dl, _ := m.settings.Downloads(ctx)
	ref := source.ChapterRef{
		Manga:     source.MangaRef{SourceID: jc.link.SourceID, URL: jc.link.MangaURL, EngineRef: jc.link.EngineRef, TitleHint: firstNonEmpty(jc.link.Title, jc.series.Title)},
		URL:       jc.release.ChapterURL,
		EngineRef: jc.release.EngineRef,
	}
	pctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	list, err := mod.Pages(pctx, ref)
	cancel()
	if err != nil {
		if errors.Is(err, source.ErrNotFound) {
			return nil, permanent(err)
		}
		return nil, classify(err)
	}
	if min := jc.profile.Config.MinPages; min > 0 && len(list) < min {
		return nil, permanent(fmt.Errorf("chapter has %d pages, profile requires at least %d", len(list), min))
	}
	pages := make([]PageFile, len(list))
	conc := dl.PageConcurrency
	if conc <= 0 {
		conc = 2
	}
	retries := dl.PageRetries
	if retries <= 0 {
		retries = 3
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	done := 0
	var fetched int64 // bytes pulled from the site, for the queue's progress
	m.progress(jc.job, 0, len(list), 0)
	for i, p := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			mu.Lock()
			stop := firstErr != nil
			mu.Unlock()
			if stop {
				return
			}
			pf, err := m.fetchPage(ctx, mod, p, i, workDir, retries)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("page %d: %w", i+1, err)
				}
				return
			}
			pages[i] = pf
			done++
			if st, err := os.Stat(pf.Path); err == nil {
				fetched += st.Size()
			}
			m.progress(jc.job, done, len(list), fetched)
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return pages, nil
}

func (m *Manager) fetchPage(ctx context.Context, mod source.Module, p source.Page, i int, workDir string, retries int) (PageFile, error) {
	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return PageFile{}, ctx.Err()
			case <-time.After(time.Duration(1<<(2*attempt)) * time.Second): // 4s, 16s
			}
		}
		fctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		body, _, err := mod.FetchPage(fctx, p)
		if err != nil {
			cancel()
			lastErr = classify(err)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(body, 64<<20))
		body.Close()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		info, err := imagecheck.Detect(data)
		if err != nil {
			lastErr = err
			continue
		}
		name := cbz.PageName(i, imagecheck.Ext(info.Format))
		path := filepath.Join(workDir, name)
		// written through a temporary name: the reader serves pages out of
		// this folder while the chapter downloads and must never see half a page
		tmp := path + ".part"
		if err := os.WriteFile(tmp, data, 0o664); err != nil {
			return PageFile{}, infraError{err}
		}
		if err := os.Rename(tmp, path); err != nil {
			return PageFile{}, infraError{err}
		}
		return PageFile{Name: name, Path: path, Format: info.Format, Width: info.Width, Height: info.Height}, nil
	}
	return PageFile{}, lastErr
}

// extractExisting unpacks an imported CBZ for re-processing.
func (m *Manager) extractExisting(jc *jobCtx, workDir string) ([]PageFile, error) {
	if jc.file == nil {
		return nil, permanent(errors.New("chapter has no file to re-process"))
	}
	dir, err := m.lib.SeriesDir(context.Background(), &jc.series)
	if err != nil {
		return nil, infraError{err}
	}
	pages, _, err := cbz.Read(filepath.Join(dir, jc.file.RelativePath))
	if err != nil {
		return nil, permanent(fmt.Errorf("read existing file: %w", err))
	}
	out := make([]PageFile, 0, len(pages))
	for i, p := range pages {
		info, err := imagecheck.Detect(p.Data)
		if err != nil {
			return nil, permanent(fmt.Errorf("existing page %s: %w", p.Name, err))
		}
		name := cbz.PageName(i, imagecheck.Ext(info.Format))
		path := filepath.Join(workDir, name)
		if err := os.WriteFile(path, p.Data, 0o664); err != nil {
			return nil, infraError{err}
		}
		out = append(out, PageFile{Name: name, Path: path, Format: info.Format, Width: info.Width, Height: info.Height})
	}
	return out, nil
}

func (m *Manager) importChapter(ctx context.Context, jc *jobCtx, proc ProcessResult, params string, sizeBefore int64) error {
	defer library.RLockSeries(jc.series.ID)() // a move/rename waits for the import
	// the series may have moved while pages were downloading
	var loc model.Series
	if err := m.db.NewSelect().Model(&loc).Column("root_folder_id", "path").Where("id = ?", jc.series.ID).Scan(ctx); err == nil {
		jc.series.RootFolderID, jc.series.Path = loc.RootFolderID, loc.Path
	}
	if jc.file != nil { // and its file may have been renamed
		var f model.ChapterFile
		if err := m.db.NewSelect().Model(&f).Column("relative_path").Where("id = ?", jc.file.ID).Scan(ctx); err == nil {
			jc.file.RelativePath = f.RelativePath
		}
	}
	pages, upscaled, upscaleModel := proc.Pages, proc.Upscaled, proc.UpscaleModel
	mm, _ := m.settings.MediaManagement(ctx)
	dir, err := m.lib.EnsureSeriesDir(ctx, &jc.series)
	if err != nil {
		return infraError{err}
	}
	if mm.MinFreeSpaceMB > 0 {
		if free, err := fsutil.FreeSpace(dir); err == nil && free < uint64(mm.MinFreeSpaceMB)<<20 {
			return infraError{fmt.Errorf("not enough free space in %s (%d MB free, %d MB required)", dir, free>>20, mm.MinFreeSpaceMB)}
		}
	}
	// Keep the existing path on upgrades/re-processing so reader servers keep
	// book ids and read progress.
	rel := ""
	if jc.file != nil {
		rel = jc.file.RelativePath
	} else {
		sourceName := ""
		if jc.link != nil {
			sourceName = jc.link.SourceName
		}
		rel = m.lib.ChapterFileName(ctx, &jc.series, &jc.chapter, jc.release, sourceName)
	}
	target := filepath.Join(dir, rel)
	// re-encoding to save space may skip the recycle bin (the whole point is
	// to free the space); everything else keeps the replaced file around
	recycle := !(jc.job.Kind == model.JobKindReprocess && proc.Encoded > 0 && !jc.profile.Config.Encode.RecycleOriginals)
	if _, err := os.Stat(target); err == nil && recycle {
		if _, err := m.lib.Recycle(ctx, target, jc.series.Path, true); err != nil {
			m.log.Warn("recycle previous file", "path", target, "err", err)
		}
	}

	ci := m.comicInfo(ctx, jc, len(pages), mm.WriteVolume)
	xmlData, err := ci.Marshal()
	if err != nil {
		return err
	}
	fmode, _ := m.lib.Modes(ctx)
	cbzPages := make([]cbz.Page, len(pages))
	widthSum := 0
	formats := map[string]int{}
	for i, p := range pages {
		cbzPages[i] = cbz.Page{Name: p.Name, Path: p.Path}
		widthSum += p.Width
		formats[p.Format]++
	}
	res, err := cbz.Write(target, cbzPages, xmlData, fmode, time.Now())
	if err != nil {
		return infraError{fmt.Errorf("write %s: %w", target, err)}
	}
	format := ""
	for f, n := range formats {
		if n > formats[format] {
			format = f
		}
	}
	now := time.Now().UTC()
	file := &model.ChapterFile{ChapterID: jc.chapter.ID, SeriesID: jc.series.ID, RelativePath: rel, Size: res.Size, PageCount: len(pages),
		Format: format, SHA256: res.SHA256, Upscaled: upscaled, UpscaleModel: upscaleModel, ImportedAt: now}
	if len(pages) > 0 {
		file.AvgWidth = widthSum / len(pages)
	}
	if upscaled {
		file.SizeBefore = sizeBefore
	}
	file.SizeOriginal = res.Size
	if proc.Changed {
		file.SizeOriginal = sizeBefore // pages as downloaded
	}
	if jc.job.Kind == model.JobKindReprocess && jc.file != nil {
		file.SizeOriginal = jc.file.SizeOriginal
		if file.SizeOriginal == 0 {
			file.SizeOriginal = jc.file.Size
		}
	}
	if params != "" {
		file.ProcessParams, file.ProcessState, file.ProcessedAt = params, model.ProcessDone, &now
		file.ProcessSeconds, file.ProcessPages = proc.Seconds, proc.ProcessedPages
	}
	if jc.release != nil {
		file.ReleaseID, file.Scanlator = &jc.release.ID, jc.release.Scanlator
	}
	if jc.link != nil {
		file.SourceName = jc.link.SourceName
	}
	if jc.job.Kind == model.JobKindReprocess && jc.file != nil {
		file.ReleaseID, file.Scanlator, file.SourceName = jc.file.ReleaseID, jc.file.Scanlator, jc.file.SourceName
		if !upscaled {
			file.Upscaled, file.UpscaleModel = jc.file.Upscaled, jc.file.UpscaleModel
		}
	}
	// Writing the very same bytes back (a re-import, or processing that turned
	// out to change nothing) keeps the existing row: its id is what reader
	// servers hang book ids and read progress on.
	if jc.file != nil && jc.file.RelativePath == rel && jc.file.SHA256 == res.SHA256 {
		file.ID, file.ImportedAt = jc.file.ID, jc.file.ImportedAt
	}
	event := model.HistoryImported
	if jc.file != nil {
		event = model.HistoryUpgraded
		if jc.job.Kind == model.JobKindReprocess {
			event = model.HistoryProcessed
		}
	}
	err = m.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if file.ID != 0 {
			if _, err := tx.NewUpdate().Model(file).WherePK().Exec(ctx); err != nil {
				return err
			}
		} else {
			if _, err := tx.NewDelete().Model((*model.ChapterFile)(nil)).Where("chapter_id = ?", jc.chapter.ID).Exec(ctx); err != nil {
				return err
			}
			if _, err := tx.NewInsert().Model(file).Exec(ctx); err != nil {
				return err
			}
		}
		if _, err := tx.NewUpdate().Model((*model.Chapter)(nil)).Set("file_id = ?", file.ID).Set("state = ?", model.ChapterImported).
			Set("cleaned_at = NULL").Set("updated_at = ?", now).Where("id = ?", jc.chapter.ID).Exec(ctx); err != nil {
			return err
		}
		jc.job.Status, jc.job.Progress, jc.job.Error, jc.job.UpdatedAt = model.JobCompleted, 100, "", now
		if _, err := tx.NewUpdate().Model(jc.job).Column("status", "progress", "error", "updated_at").WherePK().Exec(ctx); err != nil {
			return err
		}
		src := ""
		if jc.release != nil {
			src = jc.release.Name
		}
		chID := jc.chapter.ID
		return history.Record(ctx, tx, jc.series.ID, &chID, event, src, map[string]string{
			"path": rel, "size": strconv.FormatInt(res.Size, 10), "pages": strconv.Itoa(len(pages)),
			"source": file.SourceName, "scanlator": file.Scanlator, "upscaled": strconv.FormatBool(upscaled),
			"encoded": strconv.Itoa(proc.Encoded), "sizeOriginal": strconv.FormatInt(file.SizeOriginal, 10)})
	})
	if err != nil {
		return err
	}
	evType := events.ChapterImported
	if event == model.HistoryUpgraded {
		evType = events.ChapterUpgraded
	}
	if event != model.HistoryUpscaled && event != model.HistoryProcessed {
		m.bus.Publish(events.Event{Type: evType, SeriesID: jc.series.ID, Payload: events.ChapterImportedPayload{
			SeriesTitle: jc.series.Title, Chapter: jc.chapter.NumberKey, NumberSort: jc.chapter.NumberSort, Title: jc.chapter.Title,
			Source: file.SourceName, Scanlator: file.Scanlator, Upgrade: event == model.HistoryUpgraded, Upscaled: upscaled,
			CoverURL: jc.series.Metadata.CoverURL}})
	}
	m.bus.Publish(events.Event{Type: EventFileWritten, SeriesID: jc.series.ID, Payload: target})
	if proc.Encoded > 0 {
		m.bus.Publish(events.Event{Type: EventFileEncoded, SeriesID: jc.series.ID, Payload: EncodedPayload{Path: target, Format: file.Format}})
	}
	m.bus.Changed("chapter", "updated", jc.chapter.ID)
	m.bus.Changed("series", "updated", jc.series.ID)
	m.bus.Changed("queue", "updated", jc.job.ID)
	return nil
}

// EventFileWritten is published (payload: absolute path) whenever a library
// file is written or deleted, so library modules can rescan.
const EventFileWritten = "library.file"

// EventFileEncoded is published when pages were re-encoded (payload EncodedPayload).
const EventFileEncoded = "library.encoded"

type EncodedPayload struct {
	Path   string `json:"path"`
	Format string `json:"format"`
}

func (m *Manager) comicInfo(ctx context.Context, jc *jobCtx, pageCount int, writeVolume bool) comicinfo.ComicInfo {
	s := jc.series
	in := comicinfo.Input{
		SeriesTitle: s.Title, ChapterNumberKey: jc.chapter.NumberKey, ChapterTitle: jc.chapter.Title,
		Summary: s.Metadata.Description, Authors: s.Metadata.Authors, Artists: s.Metadata.Artists, Publisher: s.Metadata.Publisher,
		Genres: s.Metadata.Genres, Tags: s.Metadata.Tags, PageCount: pageCount, Language: s.Language,
		ReadingDirection: s.ReadingDirection, AgeRating: s.Metadata.AgeRating, ReleaseDate: jc.chapter.ReleaseDate,
	}
	if writeVolume {
		in.Volume = jc.chapter.Volume
	}
	if jc.release != nil {
		in.Scanlator = jc.release.Scanlator
		if jc.release.UploadDate != nil {
			in.ReleaseDate = jc.release.UploadDate
		}
		in.WebLinks = append(in.WebLinks, jc.release.WebURL)
	} else if jc.file != nil {
		in.Scanlator = jc.file.Scanlator
	}
	if u := s.Metadata.Links["AniList"]; u != "" {
		in.WebLinks = append(in.WebLinks, u)
	}
	if s.Status == model.StatusCompleted || s.Status == model.StatusCancelled {
		in.TotalCount = s.Metadata.TotalChapters
		if in.TotalCount == 0 {
			n, _ := m.db.NewSelect().Model((*model.Chapter)(nil)).Where("series_id = ?", s.ID).Count(ctx)
			in.TotalCount = n
		}
	}
	if jc.link != nil {
		in.Notes = "Downloaded by mangarr from " + jc.link.SourceName
	}
	return comicinfo.Build(in)
}

func classify(err error) error {
	if err == nil {
		return nil
	}
	var ne net.Error
	msg := strings.ToLower(err.Error())
	if errors.As(err, &ne) || strings.Contains(msg, "connection refused") || strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "unauthorized (check") {
		return infraError{err}
	}
	return err
}

var retryDelays = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// fail handles a failed attempt: retry, fall back to another release, or fail.
func (m *Manager) fail(ctx context.Context, job *model.DownloadJob, jc *jobCtx, err error) {
	if ctx.Err() != nil {
		// cancelled by the user or shutting down; the queue entry decides what happens
		return
	}
	bg := context.Background()
	dl, _ := m.settings.Downloads(bg)
	maxAttempts := dl.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	now := time.Now().UTC()
	job.Error = err.Error()
	job.UpdatedAt = now
	// the site throttled us: retry when the catalog's cooldown ends, without
	// counting it against the release
	if until := m.cooldownUntil(err, jc); !until.IsZero() {
		job.Status, job.NotBefore = model.JobQueued, until
		_, _ = m.db.NewUpdate().Model(job).Column("status", "error", "not_before", "updated_at", "release_id").WherePK().Where("status <> ?", model.JobPaused).Exec(bg)
		m.log.Info("source is cooling down, download postponed", "job", job.ID, "until", until.Local().Format(time.TimeOnly))
		m.bus.Changed("queue", "updated", job.ID)
		return
	}
	job.Attempt++
	var perm permanentError
	var infra infraError
	isPerm := errors.As(err, &perm)
	isInfra := errors.As(err, &infra)
	m.log.Warn("download attempt failed", "job", job.ID, "attempt", job.Attempt, "err", err, "permanent", isPerm)

	requeue := func(delay time.Duration) {
		job.Status, job.NotBefore = model.JobQueued, now.Add(delay)
		_, _ = m.db.NewUpdate().Model(job).Column("status", "attempt", "error", "not_before", "updated_at", "release_id").WherePK().Where("status <> ?", model.JobPaused).Exec(bg)
		m.bus.Changed("queue", "updated", job.ID)
	}
	switch {
	case isInfra:
		// our side is broken: keep retrying with growing delay, never blocklist
		d := time.Duration(job.Attempt) * 5 * time.Minute
		if d > time.Hour {
			d = time.Hour
		}
		requeue(d)
		return
	case !isPerm && job.Attempt < maxAttempts && job.Kind == model.JobKindDownload:
		requeue(retryDelays[min(job.Attempt-1, len(retryDelays)-1)])
		return
	}
	if job.Kind == model.JobKindDownload && job.ReleaseID != nil {
		if berr := BlocklistRelease(bg, m.db, *job.ReleaseID, err.Error()); berr != nil {
			m.log.Warn("blocklist release", "err", berr)
		}
		if jc != nil {
			if cand, upgrade, cerr := m.searcher.NextCandidate(bg, jc.series.ID, jc.chapter.ID); cerr == nil && cand != nil {
				rid := cand.Release.ID
				job.ReleaseID, job.IsUpgrade, job.Attempt = &rid, upgrade, 0
				_, _ = m.db.NewUpdate().Model(job).Column("is_upgrade").WherePK().Exec(bg)
				m.log.Info("trying next release", "job", job.ID, "source", cand.Source.SourceName, "scanlator", cand.Release.Scanlator)
				requeue(5 * time.Second)
				return
			}
		}
	}
	job.Status = model.JobFailed
	_, _ = m.db.NewUpdate().Model(job).Column("status", "attempt", "error", "updated_at").WherePK().Where("status <> ?", model.JobPaused).Exec(bg)
	if job.Kind == model.JobKindDownload {
		_ = resetChapterState(bg, m.db, job.ChapterID)
		var ch model.Chapter
		if m.db.NewSelect().Model(&ch).Where("id = ?", job.ChapterID).Scan(bg) == nil && ch.FileID == nil {
			_, _ = m.db.NewUpdate().Model((*model.Chapter)(nil)).Set("state = ?", model.ChapterFailed).Where("id = ?", ch.ID).Exec(bg)
		}
	}
	if jc != nil {
		chID := jc.chapter.ID
		_ = history.Record(bg, m.db, jc.series.ID, &chID, model.HistoryFailed, "", map[string]string{"error": err.Error()})
		m.bus.Publish(events.Event{Type: events.DownloadFailed, SeriesID: jc.series.ID, Payload: events.MessagePayload{
			Title:   "Download failed",
			Message: fmt.Sprintf("%s ch. %s: %v", jc.series.Title, jc.chapter.NumberKey, err)}})
	}
	m.bus.Changed("queue", "updated", job.ID)
	m.bus.Changed("chapter", "updated", job.ChapterID)
}

// cooldownUntil returns when a throttled catalog may be used again (zero
// when err isn't about throttling).
func (m *Manager) cooldownUntil(err error, jc *jobCtx) time.Time {
	if cd, ok := sourcegov.CoolingDown(err); ok {
		return cd.Until
	}
	if m.Gov == nil || jc == nil || jc.link == nil {
		return time.Time{}
	}
	if _, throttled := sourcegov.Classify(err); !throttled {
		return time.Time{}
	}
	until, _ := m.Gov.Cooldown(sourcegov.Key{ModuleID: jc.link.ModuleID, SourceID: jc.link.SourceID})
	return until
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// workDir is where a job's pages are written while it runs.
func (m *Manager) workDir(jobID int64) string {
	return filepath.Join(m.dataDir, "staging", "job-"+strconv.FormatInt(jobID, 10))
}

// StagedPage is the path of page n of a chapter being downloaded right now,
// so a reader waiting for it gets the page the downloader already fetched
// instead of asking the source for it a second time. It returns "" when
// there's no such page yet.
func (m *Manager) StagedPage(ctx context.Context, chapterID int64, n int) string {
	if n < 1 {
		return ""
	}
	var job model.DownloadJob
	err := m.db.NewSelect().Model(&job).Column("id", "status").
		Where("chapter_id = ? AND status = ?", chapterID, model.JobDownloading).Limit(1).Scan(ctx)
	if err != nil {
		return ""
	}
	dir := m.workDir(job.ID)
	for _, format := range []string{"jpeg", "png", "webp", "avif", "jxl", "gif"} {
		ext := imagecheck.Ext(format)
		path := filepath.Join(dir, cbz.PageName(n-1, ext))
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			return path
		}
	}
	return ""
}
