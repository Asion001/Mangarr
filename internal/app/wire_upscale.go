package app

import (
	"context"
	"fmt"

	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/jobs"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/upscaling"
)

// wireUpscale connects the upscaling processor to the download manager and
// registers the "upscale existing chapters" command.
func (a *App) wireUpscale(ctx context.Context) error {
	a.Downloads.Processor = upscaling.New(a.Modules)
	a.Queue.Register(jobs.Definition{Name: "UpscaleExisting", Description: "Re-process already downloaded chapters with the profile's upscaling settings",
		Handler: func(ctx context.Context, r *jobs.Run) error {
			var body struct {
				SeriesID   int64   `json:"seriesId"`
				ChapterIDs []int64 `json:"chapterIds"`
				Force      bool    `json:"force"`
			}
			if err := r.Body(&body); err != nil || body.SeriesID == 0 {
				return fmt.Errorf("seriesId required")
			}
			var files []model.ChapterFile
			q := a.DB.NewSelect().Model(&files).Where("series_id = ?", body.SeriesID)
			if len(body.ChapterIDs) > 0 {
				q = q.Where("chapter_id IN (?)", bun.In(body.ChapterIDs))
			}
			if !body.Force {
				q = q.Where("upscaled = ?", false)
			}
			if err := q.Scan(ctx); err != nil {
				return err
			}
			n := 0
			for _, f := range files {
				if _, created, err := a.DLQueue.Enqueue(ctx, body.SeriesID, f.ChapterID, f.ReleaseID, model.JobKindReprocess, true); err != nil {
					return err
				} else if created {
					n++
				}
			}
			r.Progress("queued %d chapters for upscaling", n)
			return nil
		}})
	return nil
}
