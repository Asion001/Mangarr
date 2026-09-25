package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

func TestDispatcherUsesQueueRank(t *testing.T) {
	for dialect, dsn := range dbtest.DSNs(t) {
		t.Run(dialect, func(t *testing.T) {
			name := "rank-dispatch-" + dialect
			sc := fakesource.NewScenario(name)
			sc.Sources = []source.SourceInfo{{ID: "copper", Name: "Copper Source", Lang: "en"}}
			var chapters []fakesource.Chapter
			for i := 1; i <= 4; i++ {
				chapters = append(chapters, fakesource.Chapter{URL: fmt.Sprintf("/c%d", i), Name: "Chapter", Number: float64(i), Uploaded: time.Now()})
			}
			sc.AddManga(&fakesource.Manga{SourceID: "copper", URL: "/copper", Title: "Copper Clouds", Status: source.StatusOngoing, Chapters: chapters})
			e := newTestApp(t, dsn)
			dl, err := e.App.Settings.Downloads(e.Ctx)
			if err != nil {
				t.Fatal(err)
			}
			dl.MaxConcurrent, dl.MaxConcurrentProcessing, dl.MaxPerSource = 1, 1, 1
			if err := e.App.Settings.Set(e.Ctx, settings.KeyDownloads, dl); err != nil {
				t.Fatal(err)
			}
			mod := e.addFakeModule(t, name)
			ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Copper Clouds", RootFolderID: e.RFID, Monitor: model.MonitorNone,
				Sources: []series.SourceLink{{ModuleID: mod, SourceID: "copper", URL: "/copper", SourceName: "Copper Source", Lang: "en"}}})
			if err != nil {
				t.Fatal(err)
			}
			e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
			var chs []model.Chapter
			if err := e.App.DB.NewSelect().Model(&chs).Where("series_id = ?", ser.ID).Order("number_sort").Scan(e.Ctx); err != nil {
				t.Fatal(err)
			}
			if len(chs) != 4 {
				t.Fatalf("chapters: %d", len(chs))
			}
			for _, kind := range []string{model.JobKindDownload, model.JobKindReprocess} {
				if err := e.App.Settings.Set(e.Ctx, settings.KeyQueueState, settings.QueueState{Paused: true}); err != nil {
					t.Fatal(err)
				}
				var jobs []*model.DownloadJob
				for _, ch := range chs {
					job, created, err := e.App.DLQueue.Enqueue(e.Ctx, ser.ID, ch.ID, nil, kind, kind == model.JobKindReprocess)
					if err != nil || !created {
						t.Fatalf("enqueue: %v %v", created, err)
					}
					jobs = append(jobs, job)
				}
				if n, err := e.App.DLQueue.Move(e.Ctx, []int64{jobs[3].ID, jobs[2].ID}, "top", 0); err != nil || n != 2 {
					t.Fatalf("move: %d %v", n, err)
				}
				if err := e.App.Settings.Set(e.Ctx, settings.KeyQueueState, settings.QueueState{}); err != nil {
					t.Fatal(err)
				}
				e.App.DLQueue.Wake()
				waitFor(t, 20*time.Second, kind+" jobs completed", func() bool {
					current := e.jobs(t)
					for _, job := range jobs {
						if current[job.ID].Status != model.JobCompleted {
							return false
						}
					}
					return true
				})
				current := e.jobs(t)
				var previous time.Time
				for _, i := range []int{2, 3, 0, 1} {
					job := current[jobs[i].ID]
					// Each kind has one slot, so completion order is dispatch order.
					if !job.UpdatedAt.After(previous) {
						t.Fatalf("%s dispatch ignored rank: %+v", kind, current)
					}
					previous = job.UpdatedAt
				}
			}
		})
	}
}
