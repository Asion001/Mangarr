package app_test

import (
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

func (e *testEnv) jobs(t *testing.T) map[int64]model.DownloadJob {
	t.Helper()
	var list []model.DownloadJob
	if err := e.App.DB.NewSelect().Model(&list).Scan(e.Ctx); err != nil {
		t.Fatal(err)
	}
	out := map[int64]model.DownloadJob{}
	for _, j := range list {
		out[j.ID] = j
	}
	return out
}

func (e *testEnv) countStatus(t *testing.T, status string) int {
	n := 0
	for _, j := range e.jobs(t) {
		if j.Status == status {
			n++
		}
	}
	return n
}

// TestQueuePauseAndBulk covers the global pause, pausing a running job
// (it must stay paused even though its goroutine is still finishing), bulk
// resume, priorities and filter-based selection.
func TestQueuePauseAndBulk(t *testing.T) {
	dsn := dbtest.DSNs(t)["sqlite"]
	sc := fakesource.NewScenario("queue-mgmt")
	sc.PageDelay = 150 * time.Millisecond
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	var chs []fakesource.Chapter
	for i := 1; i <= 4; i++ {
		chs = append(chs, fakesource.Chapter{URL: "/c" + string(rune('0'+i)), Name: "Chapter", Number: float64(i), Uploaded: time.Now(), Pages: 4})
	}
	sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/m", Title: "Queue Test", Status: source.StatusOngoing, Chapters: chs})

	e := newTestApp(t, dsn)
	dl, _ := e.App.Settings.Downloads(e.Ctx)
	dl.MaxConcurrent, dl.MaxPerSource = 1, 1
	_ = e.App.Settings.Set(e.Ctx, settings.KeyDownloads, dl)
	// pause everything before the series is added
	if err := e.App.Settings.Set(e.Ctx, settings.KeyQueueState, settings.QueueState{Paused: true}); err != nil {
		t.Fatal(err)
	}
	mod := e.addFakeModule(t, "queue-mgmt")
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Queue Test", RootFolderID: e.RFID, Monitor: model.MonitorAll, SearchMissing: true,
		Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/m", SourceName: "Source A", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
	waitFor(t, 10*time.Second, "4 queued jobs", func() bool { return e.countStatus(t, model.JobQueued) == 4 })
	time.Sleep(700 * time.Millisecond)
	if n := e.countStatus(t, model.JobQueued); n != 4 {
		t.Fatalf("paused queue started jobs: %d queued", n)
	}

	// priorities: move the last job to the top, then resume the queue
	var ids []int64
	for id := range e.jobs(t) {
		ids = append(ids, id)
	}
	last := ids[0]
	for _, id := range ids {
		last = max(last, id)
	}
	if n, err := e.App.Downloads.Bulk(e.Ctx, []int64{last}, "top"); err != nil || n != 1 {
		t.Fatalf("top: %d %v", n, err)
	}
	page, err := e.App.DLQueue.ListPage(e.Ctx, downloads.ListFilter{}, 1, 10)
	if err != nil || page.Items[0].ID != last || page.Total != 4 || page.Counts[model.JobQueued] != 4 {
		t.Fatalf("ordering after top: %+v %v", page, err)
	}
	_ = e.App.Settings.Set(e.Ctx, settings.KeyQueueState, settings.QueueState{})
	e.App.DLQueue.Wake()
	waitFor(t, 10*time.Second, "top job running", func() bool { return e.jobs(t)[last].Status == model.JobDownloading })

	// pause the running job: it's cancelled and stays paused
	if _, err := e.App.Downloads.Bulk(e.Ctx, []int64{last}, "pause"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(800 * time.Millisecond)
	if st := e.jobs(t)[last].Status; st != model.JobPaused {
		t.Fatalf("paused running job ended up %s", st)
	}
	var ch model.Chapter
	_ = e.App.DB.NewSelect().Model(&ch).Where("id = ?", e.jobs(t)[last].ChapterID).Scan(e.Ctx)
	if ch.State != model.ChapterQueued {
		t.Fatalf("chapter of a paused job: %s", ch.State)
	}

	// select-all-matching: pause every queued job, then resume all paused ones by filter
	pending, _ := e.App.DLQueue.IDs(e.Ctx, downloads.ListFilter{Statuses: []string{model.JobQueued, model.JobDownloading}})
	_, _ = e.App.Downloads.Bulk(e.Ctx, pending, "pause")
	time.Sleep(300 * time.Millisecond)
	all, _ := e.App.DLQueue.IDs(e.Ctx, downloads.ListFilter{Statuses: []string{model.JobPaused}})
	if want := 4 - e.countStatus(t, model.JobCompleted); len(all) != want || want < 2 {
		t.Fatalf("expected %d paused, got %d", want, len(all))
	}
	if n, err := e.App.Downloads.Bulk(e.Ctx, all, "resume"); err != nil || n != len(all) {
		t.Fatalf("resume: %d %v", n, err)
	}
	waitFor(t, 30*time.Second, "all chapters imported", func() bool { return len(e.chapterFiles(t, ser.ID)) == 4 })
	if n := e.countStatus(t, model.JobCompleted); n != 4 {
		t.Fatalf("completed jobs: %d", n)
	}
}
