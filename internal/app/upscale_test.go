package app_test

import (
	"bytes"
	"image"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
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
		Chapters: []fakesource.Chapter{{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now()}}})

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
	if w := pageWidths(t, path); w[0] != 128 {
		t.Fatalf("pages not upscaled: %v", w)
	}
}

func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
