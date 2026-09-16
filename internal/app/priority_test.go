package app_test

import (
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

// TestReadingJumpsTheQueue: a chapter someone opens is lifted above the work
// already waiting, even when it was queued in the background first.
func TestReadingJumpsTheQueue(t *testing.T) {
	sc := fakesource.NewScenario("priority")
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/m", Title: "Queued", Status: source.StatusOngoing, Chapters: []fakesource.Chapter{
		{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now()}}})
	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	// paused, so the job stays queued long enough to look at it
	if err := e.App.Settings.Set(e.Ctx, settings.KeyQueueState, settings.QueueState{Paused: true}); err != nil {
		t.Fatal(err)
	}
	mod := e.addFakeModule(t, "priority")
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Queued", RootFolderID: e.RFID, Monitor: model.MonitorNone,
		Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/m", SourceName: "Source A", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
	var chs []model.Chapter
	_ = e.App.DB.NewSelect().Model(&chs).Where("series_id = ?", ser.ID).Scan(e.Ctx)
	if len(chs) == 0 {
		t.Fatal("no chapters")
	}

	// queued in the background
	if _, err := e.App.Searcher.Evaluate(e.Ctx, ser.ID, []int64{chs[0].ID}, true); err != nil {
		t.Fatal(err)
	}
	job := jobOf(t, e, chs[0].ID)
	if job.Priority != 0 {
		t.Fatalf("background job priority %d", job.Priority)
	}

	// now someone opens it
	if _, err := e.App.Searcher.EvaluateAt(e.Ctx, ser.ID, []int64{chs[0].ID}, true, model.PriorityReading); err != nil {
		t.Fatal(err)
	}
	if job := jobOf(t, e, chs[0].ID); job.Priority != model.PriorityReading {
		t.Fatalf("priority after opening the chapter: %d", job.Priority)
	}
	if n, _ := e.App.DB.NewSelect().Model((*model.DownloadJob)(nil)).Where("chapter_id = ?", chs[0].ID).Count(e.Ctx); n != 1 {
		t.Fatalf("%d jobs for one chapter", n)
	}
}

func jobOf(t *testing.T, e *testEnv, chapterID int64) model.DownloadJob {
	t.Helper()
	var job model.DownloadJob
	if err := e.App.DB.NewSelect().Model(&job).Where("chapter_id = ?", chapterID).Limit(1).Scan(e.Ctx); err != nil {
		t.Fatal(err)
	}
	return job
}
