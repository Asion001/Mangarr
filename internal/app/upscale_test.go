package app_test

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/cbz"
	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	_ "github.com/Asion001/mangarr/internal/modules/all"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
	"github.com/Asion001/mangarr/internal/testutil/fakeupscaler"
	"github.com/Asion001/mangarr/internal/upscaler"
	"github.com/Asion001/mangarr/internal/upscaling"
)

func pageWidths(t *testing.T, path string) []int {
	pages, _, err := cbz.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []int
	for _, p := range pages {
		cfg, _, err := image.DecodeConfig(bytesReader(p.Data))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, cfg.Width)
	}
	return out
}

// TestUpscaling downloads a chapter without upscaling, then enables upscaling
// and re-processes it in place through the ncnn-worker module and a worker
// running a fake engine.
func TestUpscaling(t *testing.T) {
	dsn := dbtest.DSNs(t)["sqlite"]
	worker := upscaler.NewServer(upscaler.Config{Token: "tok", TmpDir: t.TempDir(), Version: "test"}, fakeupscaler.Runner{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	ws := httptest.NewServer(worker.Handler())
	defer ws.Close()

	sc := fakesource.NewScenario("upscale")
	sc.PageWidth = 64
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/m", Title: "Tiny Pages", Status: source.StatusOngoing,
		Chapters: []fakesource.Chapter{{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now(), Pages: 20}}})

	e := newTestApp(t, dsn)
	mod := e.addFakeModule(t, "upscale")
	up := &model.ProviderDefinition{Kind: "upscale", Implementation: "ncnn-worker", Name: "GPU", Enabled: true,
		Settings: map[string]any{"url": ws.URL, "token": "tok"}}
	if err := e.App.Modules.Create(e.Ctx, up); err != nil {
		t.Fatal(err)
	}
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Tiny Pages", RootFolderID: e.RFID, Monitor: model.MonitorAll, SearchMissing: true,
		Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/m", SourceName: "Source A", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
	waitFor(t, 20*time.Second, "download", func() bool { return len(e.chapterFiles(t, ser.ID)) == 1 })
	f := e.chapterFiles(t, ser.ID)["1"]
	path := filepath.Join(e.Root, "Tiny Pages", f.RelativePath)
	if w := pageWidths(t, path); w[0] != 64 || f.Upscaled {
		t.Fatalf("expected original pages first: %v upscaled=%v", w, f.Upscaled)
	}

	var prof model.Profile
	_ = e.App.DB.NewSelect().Model(&prof).Where("id = ?", ser.ProfileID).Scan(e.Ctx)
	prof.Config.Upscale = model.UpscaleConfig{Enabled: true, MinWidth: 100, MaxWidth: 0, Model: "waifu2x-cunet", Noise: 1, Format: "png", Quality: 90}
	if _, err := e.App.DB.NewUpdate().Model(&prof).WherePK().Exec(e.Ctx); err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "UpscaleExisting", map[string]any{"seriesId": ser.ID})
	waitFor(t, 20*time.Second, "upscaled file", func() bool { return e.chapterFiles(t, ser.ID)["1"].Upscaled })
	after := e.chapterFiles(t, ser.ID)["1"]
	if after.RelativePath != f.RelativePath || after.UpscaleModel != "waifu2x-cunet" || after.AvgWidth != 128 {
		t.Fatalf("unexpected file after upscale: %+v", after)
	}
	if w := pageWidths(t, path); w[0] != 128 || len(w) != 20 {
		t.Fatalf("pages not upscaled: %v", w)
	}
	// pages go to the upscaler a few at a time
	if n := fakeupscaler.MaxBatch.Load(); n == 0 || n > int64(upscaling.ChunkPages) {
		t.Fatalf("largest batch was %d pages", n)
	}
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }

// TestReprocessWithNothingToDo checks that reprocess jobs that have no work
// (upscaling disabled, or every page already wide enough) complete without
// rewriting the chapter file.
func TestReprocessWithNothingToDo(t *testing.T) {
	dsn := dbtest.DSNs(t)["sqlite"]
	worker := upscaler.NewServer(upscaler.Config{Token: "tok", TmpDir: t.TempDir(), Version: "test"}, fakeupscaler.Runner{},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	ws := httptest.NewServer(worker.Handler())
	defer ws.Close()

	sc := fakesource.NewScenario("reprocess-noop")
	sc.PageWidth = 64
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/m", Title: "Wide Enough", Status: source.StatusOngoing,
		Chapters: []fakesource.Chapter{{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now()}}})

	e := newTestApp(t, dsn)
	mod := e.addFakeModule(t, "reprocess-noop")
	up := &model.ProviderDefinition{Kind: "upscale", Implementation: "ncnn-worker", Name: "GPU", Enabled: true,
		Settings: map[string]any{"url": ws.URL, "token": "tok"}}
	if err := e.App.Modules.Create(e.Ctx, up); err != nil {
		t.Fatal(err)
	}
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Wide Enough", RootFolderID: e.RFID, Monitor: model.MonitorAll, SearchMissing: true,
		Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/m", SourceName: "Source A", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
	waitFor(t, 20*time.Second, "download", func() bool { return len(e.chapterFiles(t, ser.ID)) == 1 })
	before := e.chapterFiles(t, ser.ID)["1"]

	reprocessDone := func(n int) func() bool {
		return func() bool {
			c, _ := e.App.DB.NewSelect().Model((*model.DownloadJob)(nil)).
				Where("kind = ? AND status = ?", model.JobKindReprocess, model.JobCompleted).Count(e.Ctx)
			return c >= n
		}
	}
	// 1. processing disabled on the profile: nothing is queued
	e.runCommand(t, "UpscaleExisting", map[string]any{"seriesId": ser.ID, "force": true})
	if n, _ := e.App.DB.NewSelect().Model((*model.DownloadJob)(nil)).Where("kind = ?", model.JobKindReprocess).Count(e.Ctx); n != 0 {
		t.Fatalf("%d reprocess jobs for a profile without processing", n)
	}

	// 2. enabled, but pages are already wider than MinWidth
	var prof model.Profile
	_ = e.App.DB.NewSelect().Model(&prof).Where("id = ?", ser.ProfileID).Scan(e.Ctx)
	prof.Config.Upscale = model.UpscaleConfig{Enabled: true, MinWidth: 32, Model: "waifu2x-cunet", Noise: 1, Format: "png", Quality: 90}
	if _, err := e.App.DB.NewUpdate().Model(&prof).WherePK().Exec(e.Ctx); err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "UpscaleExisting", map[string]any{"seriesId": ser.ID, "force": true})
	waitFor(t, 20*time.Second, "no-op reprocess", reprocessDone(1))

	after := e.chapterFiles(t, ser.ID)["1"]
	if after.ID != before.ID || after.SHA256 != before.SHA256 || after.Upscaled {
		t.Fatalf("file was rewritten: before %+v after %+v\n%s", before, after, jobsAndHistory(t, e, ser.ID))
	}
	if after.ProcessState != model.ProcessDone {
		t.Fatalf("file should be marked processed: %+v", after)
	}
	var failed int
	failed, _ = e.App.DB.NewSelect().Model((*model.DownloadJob)(nil)).Where("status = ?", model.JobFailed).Count(e.Ctx)
	if failed != 0 {
		t.Fatalf("%d failed jobs", failed)
	}
}

// TestBackgroundAVIF imports originals first, then re-encodes them to AVIF
// in the background at the same path.
func TestBackgroundAVIF(t *testing.T) {
	dsn := dbtest.DSNs(t)["sqlite"]
	sc := fakesource.NewScenario("avif")
	sc.PageWidth, sc.PageNoise = 240, true
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/m", Title: "Encoded", Status: source.StatusOngoing,
		Chapters: []fakesource.Chapter{{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now(), Pages: 3}}})
	e := newTestApp(t, dsn)
	mod := e.addFakeModule(t, "avif")
	var prof model.Profile
	_ = e.App.DB.NewSelect().Model(&prof).Where("is_default = ?", true).Scan(e.Ctx)
	prof.Config.Encode = model.EncodeConfig{Format: "avif", Preset: "fast", Grayscale: true, MinSavingsPct: 5, RecycleOriginals: false}
	prof.Config.ProcessTiming = "background"
	if _, err := e.App.DB.NewUpdate().Model(&prof).WherePK().Exec(e.Ctx); err != nil {
		t.Fatal(err)
	}
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Encoded", RootFolderID: e.RFID, ProfileID: prof.ID, Monitor: model.MonitorAll, SearchMissing: true,
		Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/m", SourceName: "Source A", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
	waitFor(t, 20*time.Second, "download", func() bool { return len(e.chapterFiles(t, ser.ID)) == 1 })
	orig := e.chapterFiles(t, ser.ID)["1"]
	if orig.Format != "png" || orig.ProcessState != "" {
		t.Fatalf("background timing must import the original first: %+v", orig)
	}
	e.runCommand(t, "ProcessBacklog", nil)
	waitFor(t, 60*time.Second, "encoded file", func() bool { return e.chapterFiles(t, ser.ID)["1"].Format == "avif" })
	f := e.chapterFiles(t, ser.ID)["1"]
	if f.RelativePath != orig.RelativePath || f.ProcessState != model.ProcessDone || f.SizeOriginal != orig.Size || f.Size >= orig.Size {
		t.Fatalf("encoded file: %+v (original %+v)", f, orig)
	}
	if f.ProcessSeconds <= 0 || f.ProcessPages != 3 {
		t.Fatalf("processing time wasn't recorded: %v s, %d pages", f.ProcessSeconds, f.ProcessPages)
	}
	waitFor(t, 5*time.Second, "finished jobs leave the live progress list", func() bool { return len(e.App.Downloads.Live.All()) == 0 })
	pages, _, err := cbz.Read(filepath.Join(e.Root, "Encoded", f.RelativePath))
	if err != nil || len(pages) != 3 || filepath.Ext(pages[0].Name) != ".avif" {
		t.Fatalf("cbz pages: %v %v", pages, err)
	}
	// nothing is queued again for the same settings
	e.runCommand(t, "ProcessBacklog", nil)
	if n, _ := e.App.DB.NewSelect().Model((*model.DownloadJob)(nil)).Where("kind = ? AND status = ?", model.JobKindReprocess, model.JobQueued).Count(e.Ctx); n != 0 {
		t.Fatalf("%d jobs queued again", n)
	}
}

// jobsAndHistory describes what ran for a series, so a test that finds an
// unexpected rewrite can say which job did it.
func jobsAndHistory(t *testing.T, e *testEnv, seriesID int64) string {
	t.Helper()
	var out strings.Builder
	var jobs []model.DownloadJob
	_ = e.App.DB.NewSelect().Model(&jobs).Where("series_id = ?", seriesID).Order("id").Scan(e.Ctx)
	out.WriteString("jobs:\n")
	for _, j := range jobs {
		fmt.Fprintf(&out, "  #%d %s %s upgrade=%v priority=%d created=%s error=%q\n",
			j.ID, j.Kind, j.Status, j.IsUpgrade, j.Priority, j.CreatedAt.Format(time.TimeOnly), j.Error)
	}
	var events []model.History
	_ = e.App.DB.NewSelect().Model(&events).Where("series_id = ?", seriesID).Order("id").Scan(e.Ctx)
	out.WriteString("history:\n")
	for _, h := range events {
		fmt.Fprintf(&out, "  %s %s %v\n", h.CreatedAt.Format(time.TimeOnly), h.EventType, h.Data)
	}
	return out.String()
}
