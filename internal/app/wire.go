package app

import (
	"context"
	"fmt"
	"time"

	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/jobs"
	"github.com/Asion001/mangarr/internal/library"
	"github.com/Asion001/mangarr/internal/metadataagg"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/refresh"
	"github.com/Asion001/mangarr/internal/series"
)

// Services created by wire.
type Services struct {
	Library   *library.Library
	Metadata  *metadataagg.Aggregator
	DLQueue   *downloads.Queue
	Searcher  *downloads.Searcher
	Refresher *refresh.Refresher
	Series    *series.Service
	Downloads *downloads.Manager
}

// wire constructs domain services and registers commands/tasks.
func (a *App) wire(ctx context.Context) error {
	log := a.Log
	a.Library = library.New(a.DB, a.Settings, a.HTTP, a.Cfg.DataDir, log.With("component", "library"))
	a.Metadata = metadataagg.New(a.Modules, log.With("component", "metadata"))
	a.DLQueue = downloads.NewQueue(a.DB, a.Bus)
	a.Searcher = downloads.NewSearcher(a.DB, a.DLQueue, log.With("component", "search"))
	a.Refresher = refresh.New(a.DB, a.Bus, a.Modules, a.Settings, a.Searcher, a.Library, log.With("component", "refresh"))
	a.Refresher.Gov = a.Catalogs.Gov
	a.Refresher.Cache, a.Refresher.Gen = a.SourceCache, a.Catalogs.Generation
	a.Downloads = downloads.NewManager(a.DB, a.Bus, a.Modules, a.Settings, a.Library, a.DLQueue, a.Searcher, log.With("component", "downloads"), a.Cfg.DataDir)
	a.Downloads.Gov = a.Catalogs.Gov
	a.AddService(a.Downloads)
	a.Series = series.New(a.DB, a.Bus, a.Library, a.Metadata, a.Modules, a.Queue, log.With("component", "series"))

	a.Queue.Register(jobs.Definition{Name: "RefreshSources", Description: "Check linked sources that are due for new chapters",
		Handler: a.Refresher.RefreshDue})
	a.Queue.Register(jobs.Definition{Name: "RefreshSeries", Description: "Refresh all sources of one series",
		Handler: func(ctx context.Context, r *jobs.Run) error {
			var body struct {
				SeriesID int64 `json:"seriesId"`
			}
			if err := r.Body(&body); err != nil || body.SeriesID == 0 {
				return fmt.Errorf("seriesId required")
			}
			res, err := a.Refresher.SyncSeries(ctx, body.SeriesID, false)
			r.Progress("%d new chapters, %d grabbed", res.NewChapters, res.Grabbed)
			return err
		}})
	a.Queue.Register(jobs.Definition{Name: "SearchMissing", Description: "Grab missing monitored chapters (optionally for one series or chapters)",
		Handler: func(ctx context.Context, r *jobs.Run) error {
			var body struct {
				SeriesID   int64   `json:"seriesId"`
				ChapterIDs []int64 `json:"chapterIds"`
				Explicit   bool    `json:"explicit"`
			}
			if err := r.Body(&body); err != nil {
				return err
			}
			ids := []int64{body.SeriesID}
			if body.SeriesID == 0 {
				if err := a.DB.NewSelect().Model((*model.Series)(nil)).Column("id").Where("monitored = ?", true).Scan(ctx, &ids); err != nil {
					return err
				}
			}
			total := 0
			for _, id := range ids {
				n, err := a.Searcher.Evaluate(ctx, id, body.ChapterIDs, body.Explicit)
				if err != nil {
					return err
				}
				total += n
			}
			r.Progress("%d chapters queued", total)
			return nil
		}})
	a.Queue.Register(jobs.Definition{Name: "RefreshMetadata", Description: "Refresh series metadata from metadata modules",
		Handler: func(ctx context.Context, r *jobs.Run) error {
			var body struct {
				SeriesID int64 `json:"seriesId"`
			}
			_ = r.Body(&body)
			var ids []int64
			if body.SeriesID > 0 {
				ids = []int64{body.SeriesID}
			} else if err := a.DB.NewSelect().Model((*model.Series)(nil)).Column("id").Scan(ctx, &ids); err != nil {
				return err
			}
			changed := 0
			for i, id := range ids {
				ok, err := a.Series.RefreshMetadata(ctx, id)
				if err != nil {
					a.Log.Warn("metadata refresh", "series", id, "err", err)
					continue
				}
				if ok {
					changed++
					if s, err := a.Series.Get(ctx, id); err == nil {
						a.Refresher.WriteSidecars(ctx, s)
					}
				}
				r.Progress("%d/%d series, %d updated", i+1, len(ids), changed)
			}
			return nil
		}})

	if err := a.Scheduler.Add(ctx, jobs.Task{Name: "RefreshSources", Interval: 10 * time.Minute, RunOnStart: true}); err != nil {
		return err
	}
	if err := a.Scheduler.Add(ctx, jobs.Task{Name: "RefreshMetadata", Interval: 24 * time.Hour}); err != nil {
		return err
	}
	return a.wireMore(ctx)
}
