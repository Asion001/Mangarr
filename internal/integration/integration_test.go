//go:build integration

// Package integration runs mangarr's modules against real services started
// by scripts/integration.sh (Suwayomi, mangarr-upscaler, Komga). Each test
// skips when its service URL is not set.
package integration

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/imagecheck"
	"github.com/Asion001/mangarr/internal/modules"
	_ "github.com/Asion001/mangarr/internal/modules/all"
	"github.com/Asion001/mangarr/internal/modules/library"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/modules/upscale"
)

func build[T any](t *testing.T, kind modules.Kind, name string, settings map[string]any) T {
	t.Helper()
	impl, ok := modules.Lookup(kind, name)
	if !ok {
		t.Fatalf("%s/%s not registered", kind, name)
	}
	s, err := modules.DecodeSettings(impl, settings)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := impl.New(modules.Deps{}, s)
	if err != nil {
		t.Fatal(err)
	}
	return inst.(T)
}

func env(t *testing.T, k string) string {
	v := os.Getenv(k)
	if v == "" {
		t.Skipf("%s not set", k)
	}
	return v
}

// TestSuwayomiMangaDex installs the Keiyoushi MangaDex extension and fetches
// one real chapter end to end through the source module interface.
func TestSuwayomiMangaDex(t *testing.T) {
	url := env(t, "MANGARR_IT_SUWAYOMI")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	m := build[source.Module](t, modules.KindSource, "suwayomi", map[string]any{"url": url})
	if err := m.Test(ctx); err != nil {
		t.Fatal(err)
	}
	em := m.(source.ExtensionManager)
	exts, err := em.Extensions(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	const pkg = "eu.kanade.tachiyomi.extension.all.mangadex"
	installed := false
	for _, e := range exts {
		if e.Pkg == pkg && e.Installed {
			installed = true
		}
	}
	if !installed {
		if err := em.InstallExtension(ctx, pkg); err != nil {
			t.Fatal(err)
		}
	}
	var sourceID string
	srcs, err := m.Sources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range srcs {
		if s.Name == "MangaDex" && s.Lang == "en" {
			sourceID = s.ID
		}
	}
	if sourceID == "" {
		t.Fatal("MangaDex (EN) source not found")
	}
	latest, err := m.(source.Latest).Latest(ctx, sourceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, mg := range latest.Mangas {
		det, chs, err := m.Manga(ctx, mg.MangaRef, true)
		if err != nil || len(chs) == 0 {
			continue
		}
		pages, err := m.Pages(ctx, source.ChapterRef{Manga: det.MangaRef, URL: chs[0].URL, EngineRef: chs[0].EngineRef})
		if err != nil || len(pages) == 0 {
			continue
		}
		body, _, err := m.FetchPage(ctx, pages[0])
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(body)
		body.Close()
		info, err := imagecheck.Detect(data)
		if err != nil {
			t.Fatalf("page is not an image: %v", err)
		}
		t.Logf("%s ch %q: %d pages, first page %s %dx%d", det.Title, chs[0].Name, len(pages), info.Format, info.Width, info.Height)
		return
	}
	t.Fatal("no chapter with pages found in the latest list")
}

// TestUpscalerWorker upscales a sample page on a real worker (lavapipe in CI).
func TestUpscalerWorker(t *testing.T) {
	url := env(t, "MANGARR_IT_UPSCALER")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	m := build[upscale.Module](t, modules.KindUpscale, "ncnn-worker", map[string]any{"url": url, "token": os.Getenv("MANGARR_IT_UPSCALER_TOKEN")})
	info, err := m.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Models) == 0 {
		t.Fatal("no models")
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 96))
	for x := 0; x < 64; x++ {
		img.Set(x, x, color.White)
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	out, err := m.Upscale(ctx, []upscale.Image{{Name: "0001.png", Data: buf.Bytes()}}, upscale.Params{Model: "realesr-animevideov3", Scale: 2, Format: "webp", Quality: 90})
	if err != nil {
		t.Fatal(err)
	}
	info2, err := imagecheck.Detect(out[0].Data)
	if err != nil || info2.Format != "webp" || info2.Width != 128 {
		t.Fatalf("unexpected output %+v %v", info2, err)
	}
}

// TestKomga checks scans and per-user progress against a real Komga whose
// library root is /data/manga. Requires MANGARR_IT_KOMGA_KEY (admin API key).
func TestKomga(t *testing.T) {
	url := env(t, "MANGARR_IT_KOMGA")
	key := env(t, "MANGARR_IT_KOMGA_KEY")
	ctx := context.Background()
	m := build[library.Module](t, modules.KindLibrary, "komga", map[string]any{"url": url, "apiKey": key,
		"pathMappings": map[string]any{os.Getenv("MANGARR_IT_KOMGA_LOCAL"): "/data/manga"}})
	if err := m.Test(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Rescan(ctx, []string{os.Getenv("MANGARR_IT_KOMGA_LOCAL") + "/x"}); err != nil && !strings.Contains(err.Error(), "no Komga library") {
		t.Fatal(err)
	}
	pr := m.(library.ProgressReader)
	if _, err := pr.ReadProgress(ctx, library.Account{Credentials: map[string]string{"apiKey": key}}, []string{os.Getenv("MANGARR_IT_KOMGA_LOCAL")}); err != nil {
		t.Fatal(err)
	}
}
