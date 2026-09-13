// Package downloads owns the download queue: deciding what to grab
// (Searcher), queueing jobs and running the fetch → validate → process →
// import pipeline (Manager).
package downloads

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/history"
	"github.com/Asion001/mangarr/internal/model"
)

var activeStatuses = []string{model.JobQueued, model.JobDownloading, model.JobProcessing, model.JobImporting}

type Queue struct {
	db   *db.DB
	bus  *events.Bus
	wake chan struct{}
}

func NewQueue(d *db.DB, bus *events.Bus) *Queue {
	return &Queue{db: d, bus: bus, wake: make(chan struct{}, 1)}
}

func (q *Queue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

// Enqueue creates a job unless the chapter already has an active one.
func (q *Queue) Enqueue(ctx context.Context, seriesID, chapterID int64, releaseID *int64, kind string, isUpgrade bool) (*model.DownloadJob, bool, error) {
	var existing model.DownloadJob
	err := q.db.NewSelect().Model(&existing).Where("chapter_id = ?", chapterID).Where("status IN (?)", bun.In(activeStatuses)).Limit(1).Scan(ctx)
	if err == nil {
		return &existing, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	now := time.Now().UTC()
	job := &model.DownloadJob{Kind: kind, SeriesID: seriesID, ChapterID: chapterID, ReleaseID: releaseID, Status: model.JobQueued,
		IsUpgrade: isUpgrade, NotBefore: now, CreatedAt: now, UpdatedAt: now}
	err = q.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if _, err := tx.NewInsert().Model(job).Exec(ctx); err != nil {
			return err
		}
		if kind == model.JobKindDownload && !isUpgrade {
			_, err := tx.NewUpdate().Model((*model.Chapter)(nil)).Set("state = ?", model.ChapterQueued).Set("updated_at = ?", now).
				Where("id = ?", chapterID).Exec(ctx)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	q.bus.Changed("queue", "created", job.ID)
	q.bus.Changed("chapter", "updated", chapterID)
	q.signal()
	return job, true, nil
}

type JobView struct {
	model.DownloadJob
	SeriesTitle string  `json:"seriesTitle"`
	Chapter     string  `json:"chapter"`
	NumberSort  float64 `json:"numberSort"`
	SourceName  string  `json:"sourceName"`
	Scanlator   string  `json:"scanlator"`
}

// List returns active jobs (plus recently failed/completed when includeDone).
func (q *Queue) List(ctx context.Context, includeDone bool) ([]JobView, error) {
	var out []JobView
	sel := q.db.NewSelect().TableExpr("download_jobs AS j").
		ColumnExpr("j.*").
		ColumnExpr("s.title AS series_title, c.number_key AS chapter, c.number_sort AS number_sort").
		ColumnExpr("COALESCE(ss.source_name, '') AS source_name, COALESCE(r.scanlator, '') AS scanlator").
		Join("JOIN series AS s ON s.id = j.series_id").
		Join("JOIN chapters AS c ON c.id = j.chapter_id").
		Join("LEFT JOIN chapter_releases AS r ON r.id = j.release_id").
		Join("LEFT JOIN series_sources AS ss ON ss.id = r.series_source_id")
	if includeDone {
		sel = sel.Where("j.status IN (?) OR j.updated_at > ?", bun.In(activeStatuses), time.Now().UTC().Add(-24*time.Hour))
	} else {
		sel = sel.Where("j.status IN (?)", bun.In(activeStatuses))
	}
	err := sel.OrderExpr("j.id").Scan(ctx, &out)
	if out == nil {
		out = []JobView{}
	}
	return out, err
}

// ActiveChapters returns chapter ids with active jobs for a series.
func (q *Queue) ActiveChapters(ctx context.Context, seriesID int64) (map[int64]bool, error) {
	var ids []int64
	err := q.db.NewSelect().Model((*model.DownloadJob)(nil)).Column("chapter_id").
		Where("series_id = ?", seriesID).Where("status IN (?)", bun.In(activeStatuses)).Scan(ctx, &ids)
	out := map[int64]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

// Remove cancels a queued/failed job (running jobs are cancelled by the manager).
func (q *Queue) Remove(ctx context.Context, id int64, blocklist bool) error {
	var job model.DownloadJob
	if err := q.db.NewSelect().Model(&job).Where("id = ?", id).Scan(ctx); err != nil {
		return err
	}
	return q.db.RunInTx(ctx, nil, func(ctx context.Context, tx bun.Tx) error {
		if blocklist && job.ReleaseID != nil {
			if err := BlocklistRelease(ctx, tx, *job.ReleaseID, "removed from queue by user"); err != nil {
				return err
			}
		}
		if _, err := tx.NewDelete().Model((*model.DownloadJob)(nil)).Where("id = ?", id).Exec(ctx); err != nil {
			return err
		}
		return resetChapterState(ctx, tx, job.ChapterID)
	})
}

// Retry requeues a failed job.
func (q *Queue) Retry(ctx context.Context, id int64) error {
	now := time.Now().UTC()
	_, err := q.db.NewUpdate().Model((*model.DownloadJob)(nil)).
		Set("status = ?", model.JobQueued).Set("error = ''").Set("attempt = 0").Set("not_before = ?", now).Set("updated_at = ?", now).
		Where("id = ? AND status = ?", id, model.JobFailed).Exec(ctx)
	q.signal()
	q.bus.Changed("queue", "updated", id)
	return err
}

// ClearFinished removes completed/failed jobs older than d.
func (q *Queue) ClearFinished(ctx context.Context, d time.Duration) error {
	_, err := q.db.NewDelete().Model((*model.DownloadJob)(nil)).
		Where("status IN (?)", bun.In([]string{model.JobCompleted, model.JobFailed})).
		Where("updated_at < ?", time.Now().UTC().Add(-d)).Exec(ctx)
	return err
}

// resetChapterState sets a chapter back to imported/missing depending on its file.
func resetChapterState(ctx context.Context, db bun.IDB, chapterID int64) error {
	var ch model.Chapter
	if err := db.NewSelect().Model(&ch).Where("id = ?", chapterID).Scan(ctx); err != nil {
		return err
	}
	state := model.ChapterMissing
	switch {
	case ch.FileID != nil:
		state = model.ChapterImported
	case ch.CleanedAt != nil:
		state = model.ChapterCleaned
	}
	_, err := db.NewUpdate().Model((*model.Chapter)(nil)).Set("state = ?", state).Set("updated_at = ?", time.Now().UTC()).
		Where("id = ?", chapterID).Exec(ctx)
	return err
}

// BlocklistRelease adds a release to the blocklist and records history.
func BlocklistRelease(ctx context.Context, db bun.IDB, releaseID int64, reason string) error {
	var r model.ChapterRelease
	if err := db.NewSelect().Model(&r).Where("id = ?", releaseID).Scan(ctx); err != nil {
		return err
	}
	b := &model.Blocklist{SeriesID: r.SeriesID, ChapterID: r.ChapterID, SeriesSourceID: r.SeriesSourceID, ChapterURL: r.ChapterURL,
		Scanlator: r.Scanlator, Reason: reason, CreatedAt: time.Now().UTC()}
	if _, err := db.NewInsert().Model(b).On("CONFLICT (series_source_id, chapter_url) DO NOTHING").Exec(ctx); err != nil {
		return err
	}
	return history.Record(ctx, db, r.SeriesID, r.ChapterID, model.HistoryBlocklist, r.Name, map[string]string{"reason": reason, "scanlator": r.Scanlator})
}
