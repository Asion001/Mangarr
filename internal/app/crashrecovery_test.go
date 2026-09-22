package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

// TestCrashWhileProcessingIsCounted: a chapter whose processing was running
// when the process died (killed for memory, say) is not run again the
// moment the server is back — that ran the same chapter into the same kill
// every twenty minutes for two days. It counts as a failed processing
// attempt, and the backlog retries it later. A job stopped by a clean
// shutdown is only handed back.
func TestCrashWhileProcessingIsCounted(t *testing.T) {
	sc := fakesource.NewScenario("crash-processing")
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/m", Title: "Heavy Pages", Status: source.StatusOngoing,
		Chapters: []fakesource.Chapter{{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now()},
			{URL: "/c2", Name: "Chapter 2", Number: 2, Uploaded: time.Now(), Pages: 10}}})

	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	mod := e.addFakeModule(t, "crash-processing")
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Heavy Pages", RootFolderID: e.RFID, Monitor: model.MonitorNone,
		Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/m", SourceName: "Source A", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
	var chs []model.Chapter
	_ = e.App.DB.NewSelect().Model(&chs).Where("series_id = ?", ser.ID).Order("number_sort").Scan(e.Ctx)
	e.runCommand(t, "SearchMissing", map[string]any{"seriesId": ser.ID, "chapterIds": []int64{chs[0].ID}, "explicit": true})
	waitFor(t, 20*time.Second, "chapter 1 downloaded", func() bool { return len(e.chapterFiles(t, ser.ID)) == 1 })
	f := e.chapterFiles(t, ser.ID)["1"]

	// a clean stop hands a running job back to the queue
	sc.Update(func() { sc.PageDelay = 500 * time.Millisecond })
	e.runCommand(t, "SearchMissing", map[string]any{"seriesId": ser.ID, "chapterIds": []int64{chs[1].ID}, "explicit": true})
	waitFor(t, 10*time.Second, "chapter 2 downloading", func() bool { return e.countStatus(t, model.JobDownloading) == 1 })
	e.App.Downloads.Hold(true)
	e.App.Downloads.Release()
	if n := e.countStatus(t, model.JobDownloading); n != 0 {
		t.Fatalf("a clean stop should hand the running download back: %d still downloading", n)
	}
	e.App.Downloads.CancelAll()

	// the process died while chapter 1 was processing
	job, _, err := e.App.DLQueue.EnqueuePriority(e.Ctx, ser.ID, f.ChapterID, f.ReleaseID, model.JobKindReprocess, true, -100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.App.DB.NewUpdate().Model((*model.DownloadJob)(nil)).Set("status = ?", model.JobProcessing).
		Where("id = ?", job.ID).Exec(e.Ctx); err != nil {
		t.Fatal(err)
	}

	// a second manager over the same database starts up like a restart would
	ctx, cancel := context.WithCancel(e.Ctx)
	m := downloads.NewManager(e.App.DB, e.App.Bus, e.App.Modules, e.App.Settings, e.App.Library, e.App.DLQueue, e.App.Searcher,
		slog.New(slog.NewTextHandler(io.Discard, nil)), e.App.Cfg.DataDir)
	m.Hold(true)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()

	got := e.jobs(t)[job.ID]
	if got.Status != model.JobFailed || got.Attempt != 1 || got.Error != downloads.ErrStoppedWhileProcessing.Error() {
		t.Fatalf("the crashed processing job should fail, not run again: %+v", got)
	}
	after := e.chapterFiles(t, ser.ID)["1"]
	if after.ProcessState != model.ProcessFailed || after.ProcessAttempts != 1 || after.ProcessRetryAt == nil || !after.ProcessRetryAt.After(time.Now()) {
		t.Fatalf("the file should wait for a later retry: %+v", after)
	}
	if n := e.countStatus(t, model.JobQueued); n != 1 {
		t.Fatalf("the handed back download should be queued: %d queued", n)
	}
}
