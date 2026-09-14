package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/health"
	"github.com/Asion001/mangarr/internal/imageenc"
	"github.com/Asion001/mangarr/internal/jobs"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/processing"
	"github.com/Asion001/mangarr/internal/quiet"
	"github.com/Asion001/mangarr/internal/upscaling"
)

// BacklogBatch is how many processing jobs the backlog keeps queued.
const BacklogBatch = 50

// BacklogPriority queues background processing behind downloads.
const BacklogPriority = -100

// wireProcess connects upscaling and re-encoding to the download manager
// and registers the background processing commands.
func (a *App) wireProcess(ctx context.Context) error {
	if a.Encoder == nil {
		a.Encoder = imageenc.Detect()
	}
	a.Processing = processing.New(upscaling.New(a.Modules), a.Encoder)
	a.Processing.Guard = processing.NewGuard(a.Settings, a.Modules, a.Bus, a.Log.With("component", "processing"))
	a.Downloads.Processor = a.Processing
	a.Health.AddCheck(a.processingHealth)
	if err := a.wireNodes(ctx); err != nil {
		return err
	}
	if err := a.wireOrganize(ctx); err != nil {
		return err
	}
	if err := a.wireImports(ctx); err != nil {
		return err
	}

	existing := func(ctx context.Context, r *jobs.Run) error {
		var body struct {
			SeriesID   int64   `json:"seriesId"`
			ChapterIDs []int64 `json:"chapterIds"`
			ProfileID  int64   `json:"profileId"`
			// Force re-processes files already processed with the current settings.
			Force bool `json:"force"`
		}
		if err := r.Body(&body); err != nil || (body.SeriesID == 0 && body.ProfileID == 0) {
			return fmt.Errorf("seriesId or profileId required")
		}
		q := a.DB.NewUpdate().Model((*model.ChapterFile)(nil)).Set("process_params = ?", model.ProcessForce).
			Set("process_attempts = 0").Set("process_retry_at = NULL")
		if body.SeriesID > 0 {
			q = q.Where("series_id = ?", body.SeriesID)
		} else {
			q = q.Where("series_id IN (SELECT id FROM series WHERE profile_id = ?)", body.ProfileID)
		}
		if len(body.ChapterIDs) > 0 {
			q = q.Where("chapter_id IN (?)", bun.In(body.ChapterIDs))
		}
		if !body.Force {
			q = q.Where("process_state <> ? OR process_params = ''", model.ProcessDone)
		}
		res, err := q.Exec(ctx)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		r.Progress("marked %d chapters for processing", n)
		queued, err := a.processBacklog(ctx)
		if err == nil {
			r.Progress("marked %d chapters for processing, queued %d", n, queued)
		}
		return err
	}
	for _, name := range []string{"ProcessExisting", "UpscaleExisting"} {
		a.Queue.Register(jobs.Definition{Name: name, Description: "Process already downloaded chapters with the profile's upscaling and re-encoding settings",
			Handler: existing})
	}
	a.Queue.Register(jobs.Definition{Name: "ProcessBacklog", Description: "Queue background upscaling/re-encoding of chapters that need it",
		Handler: func(ctx context.Context, r *jobs.Run) error {
			n, err := a.processBacklog(ctx)
			if err == nil {
				r.Progress("queued %d chapters for processing", n)
			}
			return err
		}})
	if err := a.Scheduler.Add(ctx, jobs.Task{Name: "ProcessBacklog", Interval: 30 * time.Minute}); err != nil {
		return err
	}

	// a new or re-enabled upscaler may unblock waiting chapters
	var mu sync.Mutex
	var pending *time.Timer
	a.Modules.OnChange(func() {
		mu.Lock()
		defer mu.Unlock()
		if pending != nil {
			pending.Stop()
		}
		pending = time.AfterFunc(5*time.Second, func() { a.PushProcessBacklog("modules-changed") })
	})
	return nil
}

// PushProcessBacklog asks the backlog to look for work (e.g. after profile changes).
func (a *App) PushProcessBacklog(trigger string) {
	_, _ = a.Queue.Push(context.Background(), "ProcessBacklog", nil, trigger)
}

// processBacklog queues processing jobs for files whose profile settings
// changed since they were processed (or that were never processed).
func (a *App) processBacklog(ctx context.Context) (int, error) {
	if qs, _ := a.Settings.QueueState(ctx); qs.Active(time.Now()) {
		return 0, nil
	}
	sched, _ := a.Settings.Schedule(ctx)
	if quiet.Evaluate(sched, time.Now()).PauseProcessing {
		return 0, nil
	}
	active, err := a.DB.NewSelect().Model((*model.DownloadJob)(nil)).
		Where("kind = ? AND status IN (?)", model.JobKindReprocess, bun.In(downloads.ActiveStatuses())).Count(ctx)
	if err != nil {
		return 0, err
	}
	room := BacklogBatch - active
	if room <= 0 {
		return 0, nil
	}
	var profiles []model.Profile
	if err := a.DB.NewSelect().Model(&profiles).Scan(ctx); err != nil {
		return 0, err
	}
	queued := 0
	now := time.Now().UTC()
	for _, p := range profiles {
		params := p.Config.ProcessParams()
		if params == "" || room <= 0 {
			continue
		}
		var files []model.ChapterFile
		q := a.DB.NewSelect().Model(&files).
			Where("process_params <> ?", params).
			Where("series_id IN (SELECT id FROM series WHERE profile_id = ?)", p.ID).
			Where("(process_retry_at IS NULL OR process_retry_at <= ?)", now).
			Where("process_attempts < ?", downloads.MaxProcessAttempts).
			Where("NOT EXISTS (SELECT 1 FROM download_jobs j WHERE j.chapter_id = chapter_file.chapter_id AND j.status IN (?))", bun.In(downloads.ActiveStatuses()))
		if !p.Config.ProcessExisting && p.Config.ProcessChangedAt != nil {
			// only chapters imported since processing was set up, unless asked
			q = q.Where("(imported_at >= ? OR process_params = ?)", p.Config.ProcessChangedAt.UTC(), model.ProcessForce)
		}
		if err := q.OrderExpr("imported_at DESC").Limit(room).Scan(ctx); err != nil {
			return queued, err
		}
		for _, f := range files {
			if _, created, err := a.DLQueue.EnqueuePriority(ctx, f.SeriesID, f.ChapterID, f.ReleaseID, model.JobKindReprocess, true, BacklogPriority); err != nil {
				return queued, err
			} else if created {
				queued++
				room--
			}
		}
	}
	return queued, nil
}

// processingHealth reports paused re-encoding and missing or slow encoders.
func (a *App) processingHealth(ctx context.Context) []health.Check {
	var out []health.Check
	if blocked, reason := a.Processing.Guard.Blocked(); blocked {
		out = append(out, health.Check{Source: "Processing", Type: health.Error, Link: "/settings/profiles",
			Message: "Re-encoding is paused: " + reason})
	}
	var profiles []model.Profile
	_ = a.DB.NewSelect().Model(&profiles).Scan(ctx)
	seen := map[string]bool{}
	for _, p := range profiles {
		f := p.Config.Encode.Format
		if f == "" || f == "keep" || seen[f] {
			continue
		}
		seen[f] = true
		eng, ok := a.Encoder.Engine(f)
		switch {
		case !ok:
			out = append(out, health.Check{Source: "Processing", Type: health.Warning, Link: "/settings/profiles",
				Message: fmt.Sprintf("Profile %q re-encodes to %s but no %s encoder is installed (use the full image)", p.Name, f, f)})
		case eng.Slow():
			out = append(out, health.Check{Source: "Processing", Type: health.Notice, Link: "/settings/profiles",
				Message: fmt.Sprintf("Re-encoding to %s uses the slow built-in encoder; the full image includes avifenc, which is several times faster", f)})
		}
	}
	return out
}
