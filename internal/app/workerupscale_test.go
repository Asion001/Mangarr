package app_test

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/api"
	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
	"github.com/Asion001/mangarr/internal/testutil/fakeupscaler"
	"github.com/Asion001/mangarr/internal/upscaler"
	"github.com/Asion001/mangarr/internal/upscaling"
	"github.com/Asion001/mangarr/internal/worker"
)

// TestWorkerUpscalesAChapter: with the "mangarr workers" upscaler, the
// pages go to a worker a batch at a time and come back upscaled. Nothing
// else about processing changes — the profile, the chunking and the import
// are the same as with an upscaler on this machine.
func TestWorkerUpscalesAChapter(t *testing.T) {
	sc := fakesource.NewScenario("worker-upscale")
	sc.PageWidth = 200
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/m", Title: "Small Pages", Status: source.StatusOngoing,
		Chapters: []fakesource.Chapter{
			{URL: "/c1", Name: "Chapter 1", Number: 1, Pages: 6, Uploaded: time.Now()},
			{URL: "/c2", Name: "Chapter 2", Number: 2, Pages: 6, Uploaded: time.Now()},
		}})

	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	dl, _ := e.App.Settings.Downloads(e.Ctx)
	dl.MaxConcurrentProcessing = 2
	if err := e.App.Settings.Set(e.Ctx, settings.KeyDownloads, dl); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.New(e.App))
	defer srv.Close()

	key, _, err := e.App.Auth.CreateWorker(e.Ctx, "gpu", []string{model.RoleUpscale}, 0)
	if err != nil {
		t.Fatal(err)
	}
	// the upscaler that the worker runs: the same engine the server would
	// use, on the other machine
	engine := upscaler.NewServer(upscaler.Config{TmpDir: t.TempDir(), Version: "test"}, fakeupscaler.Runner{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx, stop := context.WithCancel(e.Ctx)
	defer stop()
	wk, err := worker.New(worker.Config{ServerURL: srv.URL, Key: key, Roles: []string{model.RoleUpscale},
		Version: "test", Upscaler: engine, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = wk.Run(ctx) }()

	// the upscale module that hands batches to the workers
	up := &model.ProviderDefinition{Kind: "upscale", Implementation: "workers", Name: "Workers", Enabled: true,
		Settings: map[string]any{"timeoutMinutes": 2}}
	if err := e.App.Modules.Create(e.Ctx, up); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "the worker to say hello", func() bool {
		var w model.Worker
		if err := e.App.DB.NewSelect().Model(&w).Where("name = ?", "gpu").Scan(e.Ctx); err != nil {
			return false
		}
		return w.LastSeenAt != nil && len(w.Info) > 0
	})

	mod := e.addFakeModule(t, "worker-upscale")
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Small Pages", RootFolderID: e.RFID, Monitor: model.MonitorAll, SearchMissing: true,
		Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/m", SourceName: "Source A", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
	waitFor(t, 30*time.Second, "downloads", func() bool { return len(e.chapterFiles(t, ser.ID)) == 2 })
	first := e.chapterFiles(t, ser.ID)["1"]
	if w := pageWidths(t, filepath.Join(e.Root, "Small Pages", first.RelativePath)); w[0] != 200 || first.Upscaled {
		t.Fatalf("the pages should arrive as they are: %v upscaled=%v", w, first.Upscaled)
	}

	var prof model.Profile
	_ = e.App.DB.NewSelect().Model(&prof).Where("id = ?", ser.ProfileID).Scan(e.Ctx)
	prof.Config.Upscale = model.UpscaleConfig{Enabled: true, MinWidth: 300, Model: "waifu2x-cunet", Noise: 1, Format: "png", Quality: 90}
	if _, err := e.App.DB.NewUpdate().Model(&prof).WherePK().Exec(e.Ctx); err != nil {
		t.Fatal(err)
	}
	fakeupscaler.Delay.Store(int64(500 * time.Millisecond))
	defer fakeupscaler.Delay.Store(0)
	e.runCommand(t, "UpscaleExisting", map[string]any{"seriesId": ser.ID})
	waitFor(t, 20*time.Second, "two processing tasks leased", func() bool {
		n, _ := e.App.DB.NewSelect().Model((*model.WorkerTask)(nil)).
			Where("kind = ? AND state = ?", model.TaskUpscale, model.TaskLeased).Count(e.Ctx)
		return n >= 2
	})
	waitFor(t, 60*time.Second, "the worker to upscale both chapters", func() bool {
		files := e.chapterFiles(t, ser.ID)
		return files["1"].Upscaled && files["2"].Upscaled
	})

	after := e.chapterFiles(t, ser.ID)["1"]
	if after.UpscaleModel != "waifu2x-cunet" || after.AvgWidth != 400 {
		t.Fatalf("after upscaling on a worker: %+v", after)
	}
	path := filepath.Join(e.Root, "Small Pages", after.RelativePath)
	if w := pageWidths(t, path); len(w) != 6 || w[0] != 400 {
		t.Fatalf("pages: %v", w)
	}
	if after.RelativePath != first.RelativePath {
		t.Fatalf("the file moved: %q -> %q", first.RelativePath, after.RelativePath)
	}
	// pages go over a few at a time, so a slow GPU shows progress and a big
	// chapter doesn't become one enormous transfer
	if n := fakeupscaler.MaxBatch.Load(); n == 0 || n > int64(upscaling.ChunkPages) {
		t.Fatalf("largest batch was %d pages", n)
	}
	// it was the worker that did it
	var tasks []model.WorkerTask
	if err := e.App.DB.NewSelect().Model(&tasks).Where("kind = ?", model.TaskUpscale).Scan(e.Ctx); err != nil {
		t.Fatal(err)
	}
	if len(tasks) == 0 {
		t.Fatal("no upscaling task reached a worker")
	}
	for _, task := range tasks {
		if task.State != model.TaskDone {
			t.Fatalf("task %d ended as %s: %s", task.ID, task.State, task.Error)
		}
	}
}
