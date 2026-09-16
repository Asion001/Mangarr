//go:build integration

// Package integration runs mangarr's modules against real services started
// by scripts/integration.sh (Suwayomi, mangarr-upscaler, Komga). Each test
// skips when its service URL is not set.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/backupimport"
	"github.com/Asion001/mangarr/internal/imagecheck"
	"github.com/Asion001/mangarr/internal/modules"
	_ "github.com/Asion001/mangarr/internal/modules/all"
	"github.com/Asion001/mangarr/internal/modules/library"
	"github.com/Asion001/mangarr/internal/modules/source"
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

// TestSuwayomiBackup adds a manga to Suwayomi's library, marks chapters read,
// creates a backup and checks it maps exactly to the module's catalog ids.
func TestSuwayomiBackup(t *testing.T) {
	url := env(t, "MANGARR_IT_SUWAYOMI")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	m := build[source.Module](t, modules.KindSource, "suwayomi", map[string]any{"url": url})
	srcs, err := m.Sources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var sourceID string
	for _, s := range srcs {
		if s.Name == "MangaDex" && s.Lang == "en" {
			sourceID = s.ID
		}
	}
	if sourceID == "" {
		t.Skip("MangaDex (EN) isn't installed (run TestSuwayomiMangaDex first)")
	}
	gql := func(query string, out any) {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"query": query})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/graphql", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var env struct {
			Data   json.RawMessage `json:"data"`
			Errors []any           `json:"errors"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil || len(env.Errors) > 0 {
			t.Fatalf("%s: %v %v", query, err, env.Errors)
		}
		if out != nil {
			_ = json.Unmarshal(env.Data, out)
		}
	}
	latest, err := m.(source.Latest).Latest(ctx, sourceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, mg := range latest.Mangas {
		det, chs, err := m.Manga(ctx, mg.MangaRef, true)
		if err != nil || len(chs) < 3 || det.EngineRef == "" {
			continue
		}
		gql(fmt.Sprintf(`mutation { updateManga(input:{id:%s, patch:{inLibrary:true}}){ manga{ id } } }`, det.EngineRef), nil)
		var got struct {
			Chapters struct {
				Nodes []struct {
					ID            int     `json:"id"`
					ChapterNumber float64 `json:"chapterNumber"`
				} `json:"nodes"`
			} `json:"chapters"`
		}
		gql(fmt.Sprintf(`{ chapters(condition:{mangaId:%s}){ nodes{ id chapterNumber } } }`, det.EngineRef), &got)
		nodes := got.Chapters.Nodes
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].ChapterNumber < nodes[j].ChapterNumber })
		gql(fmt.Sprintf(`mutation { updateChapters(input:{ids:[%d,%d], patch:{isRead:true}}){ chapters{ id } } }`, nodes[0].ID, nodes[1].ID), nil)
		var bk struct {
			CreateBackup struct {
				URL string `json:"url"`
			} `json:"createBackup"`
		}
		gql(`mutation { createBackup(input:{}){ url } }`, &bk)
		resp, err := http.Get(url + bk.CreateBackup.URL)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		b, err := backupimport.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range b.Entries {
			if e.URL == det.URL {
				if e.SourceID != sourceID || e.ReadCount() != 2 || e.Title == "" {
					t.Fatalf("entry = %s %s read %d, want source %s", e.SourceID, e.URL, e.ReadCount(), sourceID)
				}
				t.Logf("%s: %d chapters, 2 read, maps to catalog %s", e.Title, len(e.Chapters), sourceID)
				return
			}
		}
		t.Fatalf("%s missing from the backup (%d entries)", det.URL, len(b.Entries))
	}
	t.Fatal("no manga with 3+ chapters in the latest list")
}
