package app

import (
	"context"
	"time"

	"github.com/Asion001/mangarr/internal/cleanup"
	"github.com/Asion001/mangarr/internal/jobs"
	"github.com/Asion001/mangarr/internal/readsync"
)

// ReaderServices are created by wireReaders.
type ReaderServices struct {
	ReadSync *readsync.Syncer
	Cleaner  *cleanup.Cleaner
}

// wireReaders registers read-progress sync and cleanup.
func (a *App) wireReaders(ctx context.Context) error {
	a.ReadSync = readsync.New(a.DB, a.Modules, a.Bus, a.Log.With("component", "readsync"))
	a.Cleaner = cleanup.New(a.DB, a.Settings, a.Library, a.Bus, a.Log.With("component", "cleanup"))

	a.Queue.Register(jobs.Definition{Name: "SyncReadProgress", Description: "Pull per-reader progress from Komga/Kavita",
		Handler: func(ctx context.Context, r *jobs.Run) error {
			res, err := a.ReadSync.Sync(ctx)
			r.Progress("%d accounts, %d changes, %d failed", res.Accounts, res.Updated, res.Failed)
			if err == nil {
				if cs, _ := a.Settings.Cleanup(ctx); cs.Enabled {
					_, _ = a.Queue.Push(ctx, "Cleanup", nil, "after-sync")
				}
			}
			return err
		}})
	a.Queue.Register(jobs.Definition{Name: "Cleanup", Description: "Delete chapters every reader has finished (read-based cleanup)",
		Handler: func(ctx context.Context, r *jobs.Run) error {
			var body struct {
				Force bool `json:"force"`
			}
			_ = r.Body(&body)
			plan, deleted, err := a.Cleaner.Run(ctx, body.Force)
			if plan != nil {
				if plan.DryRun && !body.Force {
					r.Progress("dry run: %d chapters (%d bytes) would be removed", len(plan.Candidates), plan.TotalSize)
				} else {
					r.Progress("removed %d chapters", deleted)
				}
			}
			return err
		}})
	rs, _ := a.Settings.ReadSync(ctx)
	interval := time.Duration(rs.IntervalMinutes) * time.Minute
	if interval < 5*time.Minute {
		interval = 30 * time.Minute
	}
	if err := a.Scheduler.Add(ctx, jobs.Task{Name: "SyncReadProgress", Interval: interval}); err != nil {
		return err
	}
	return a.wireProcess(ctx)
}
