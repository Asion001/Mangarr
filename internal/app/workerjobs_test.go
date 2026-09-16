package app_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
	"github.com/Asion001/mangarr/internal/worktasks"
)

// TestRestartLeavesWorkerJobs: restarting the server requeues what it was
// doing itself, but a job a worker still holds is the worker's — its pages
// are arriving in staging while we start up.
func TestRestartLeavesWorkerJobs(t *testing.T) {
	sc := fakesource.NewScenario("worker-restart")
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/m", Title: "Held", Status: source.StatusOngoing,
		Chapters: []fakesource.Chapter{{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now()},
			{URL: "/c2", Name: "Chapter 2", Number: 2, Uploaded: time.Now()}}})

	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	mod := e.addFakeModule(t, "worker-restart")
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Held", RootFolderID: e.RFID, Monitor: model.MonitorNone, NoRefresh: true,
		Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/m", SourceName: "Source A", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})

	var chapters []model.Chapter
	if err := e.App.DB.NewSelect().Model(&chapters).Where("series_id = ?", ser.ID).Order("number_sort").Scan(e.Ctx); err != nil || len(chapters) != 2 {
		t.Fatalf("chapters: %v %d", err, len(chapters))
	}
	// two jobs that were in flight when the server stopped: one ours, one a
	// worker's
	now := time.Now().UTC()
	var jobs []*model.DownloadJob
	for _, ch := range chapters {
		j := &model.DownloadJob{Kind: model.JobKindDownload, Status: model.JobDownloading, SeriesID: ser.ID, ChapterID: ch.ID,
			NotBefore: now, CreatedAt: now, UpdatedAt: now}
		if _, err := e.App.DB.NewInsert().Model(j).Exec(e.Ctx); err != nil {
			t.Fatal(err)
		}
		jobs = append(jobs, j)
	}
	mine, theirs := jobs[0], jobs[1]
	worker := &model.Worker{Name: "far-away", KeyHash: "h", Prefix: "mgw_x", Roles: []string{model.RoleDownload},
		Enabled: true, Info: map[string]any{}, CreatedAt: now}
	if _, err := e.App.DB.NewInsert().Model(worker).Exec(e.Ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.App.Tasks.Add(e.Ctx, &model.WorkerTask{JobID: theirs.ID, Kind: model.TaskDownload}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.App.Tasks.Claim(e.Ctx, worker.ID, []string{model.TaskDownload}); err != nil {
		t.Fatal(err)
	}
	// both have a working directory with a page in it
	staging := filepath.Join(e.App.Cfg.DataDir, "staging")
	for _, j := range jobs {
		dir := filepath.Join(staging, "job-"+strconv.FormatInt(j.ID, 10))
		if err := os.MkdirAll(dir, 0o775); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "0001.jpg"), []byte("page"), 0o664); err != nil {
			t.Fatal(err)
		}
	}

	// a second manager over the same database starts up like a restart would
	ctx, cancel := context.WithCancel(e.Ctx)
	m := downloads.NewManager(e.App.DB, e.App.Bus, e.App.Modules, e.App.Settings, e.App.Library, e.App.DLQueue, e.App.Searcher,
		slog.New(slog.NewTextHandler(io.Discard, nil)), e.App.Cfg.DataDir)
	m.Tasks = e.App.Tasks
	m.Hold(true) // it may recover jobs, but it must not start any
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()

	get := func(id int64) model.DownloadJob {
		t.Helper()
		var j model.DownloadJob
		if err := e.App.DB.NewSelect().Model(&j).Where("id = ?", id).Scan(e.Ctx); err != nil {
			t.Fatal(err)
		}
		return j
	}
	if got := get(mine.ID); got.Status != model.JobQueued {
		t.Fatalf("our own interrupted job should be queued again: %+v", got)
	}
	if got := get(theirs.ID); got.Status != model.JobDownloading {
		t.Fatalf("a job a worker holds should be left alone: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(staging, "job-"+strconv.FormatInt(mine.ID, 10))); !os.IsNotExist(err) {
		t.Fatalf("our own staging should be gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "job-"+strconv.FormatInt(theirs.ID, 10), "0001.jpg")); err != nil {
		t.Fatalf("the worker's pages were thrown away: %v", err)
	}

	// once the worker's task is finished, the job is ours again
	held, err := e.App.Tasks.OfJob(e.Ctx, theirs.ID)
	if err != nil || len(held) != 1 {
		t.Fatalf("tasks: %v %+v", err, held)
	}
	if err := e.App.Tasks.Finish(e.Ctx, held[0].ID, worker.ID, worktasks.Progress{PagesDone: 3}); err != nil {
		t.Fatal(err)
	}
	if open, err := e.App.Tasks.OpenJobs(e.Ctx); err != nil || open[theirs.ID] {
		t.Fatalf("still open: %v %+v", err, open)
	}
}
