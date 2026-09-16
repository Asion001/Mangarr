package native

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// fakeSite is a site with one manga, used to check the adapter rather than
// any real site's quirks.
type fakeSite struct {
	base  string
	lang  string
	pages []sourcekit.PageImage
}

func (f *fakeSite) Info() sourcekit.Info {
	return sourcekit.Info{ID: "fake", Name: "Fake", Lang: f.lang, BaseURL: f.base, SupportsBrowse: true}
}

func (f *fakeSite) Search(_ context.Context, q string, _ int) (sourcekit.Results, error) {
	if q == "nothing" {
		return sourcekit.Results{}, nil
	}
	return sourcekit.Results{Mangas: []sourcekit.Manga{{URL: "/m/1", Title: "Fake Manga", CoverURL: f.base + "/cover.jpg"}}, HasNext: true}, nil
}

func (f *fakeSite) Popular(ctx context.Context, page int) (sourcekit.Results, error) {
	return f.Search(ctx, "", page)
}
func (f *fakeSite) Latest(ctx context.Context, page int) (sourcekit.Results, error) {
	return f.Search(ctx, "", page)
}

func (f *fakeSite) Details(_ context.Context, ref sourcekit.Ref) (sourcekit.Details, error) {
	if ref.URL != "/m/1" {
		return sourcekit.Details{}, sourcekit.ErrNotFound
	}
	return sourcekit.Details{Manga: sourcekit.Manga{URL: "/m/1", Title: "Fake Manga", CoverURL: f.base + "/cover.jpg"},
		Author: "A", Artist: "B", Description: "About it", Genres: []string{"Action"}, Status: sourcekit.StatusOngoing,
		WebURL: f.base + "/m/1"}, nil
}

func (f *fakeSite) Chapters(context.Context, sourcekit.Ref) ([]sourcekit.Chapter, error) {
	at := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	return []sourcekit.Chapter{{URL: "/c/2", Name: "Ch. 2", Number: 2, Scanlator: "Group", UploadedAt: &at}}, nil
}

func (f *fakeSite) Pages(context.Context, string) ([]sourcekit.PageImage, error) { return f.pages, nil }

func (f *fakeSite) Options() []sourcekit.Option {
	return []sourcekit.Option{{Key: "lang", Title: "Language", Type: "select", Value: f.lang,
		Choices: []sourcekit.Choice{{Value: "en", Label: "English"}, {Value: "de", Label: "German"}}}}
}

func (f *fakeSite) SetOption(key string, value any) error {
	if key != "lang" {
		return sourcekit.ErrUnsupported
	}
	f.lang, _ = value.(string)
	return nil
}

func newModule(t *testing.T, base string, pages []sourcekit.PageImage) (*Module, *fakeSite) {
	t.Helper()
	site := &fakeSite{base: base, lang: "en", pages: pages}
	m := &Module{log: slog.New(slog.NewTextHandler(io.Discard, nil)), sites: []sourcekit.Site{site},
		byID: map[string]sourcekit.Site{"fake": site}, optionsPath: filepath.Join(t.TempDir(), "native-1.json")}
	return m, site
}

// TestModuleServesASite: a site shows up as a catalog and its manga, chapters
// and pages come through the module contract unchanged.
func TestModuleServesASite(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" || r.Header.Get("User-Agent") == "" {
			http.Error(w, "the site expects a browser", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-bytes"))
	}))
	defer srv.Close()
	pages := []sourcekit.PageImage{{Index: 0, URL: srv.URL + "/p/1.jpg", Headers: map[string]string{"Referer": srv.URL + "/"}}}
	m, _ := newModule(t, srv.URL, pages)
	ctx := context.Background()

	cats, err := m.Sources(ctx)
	if err != nil || len(cats) != 1 || cats[0].ID != "fake" || !cats[0].SupportsLatest {
		t.Fatalf("catalogs: %v %+v", err, cats)
	}
	res, err := m.Search(ctx, "fake", "anything", 1)
	if err != nil || len(res.Mangas) != 1 || res.Mangas[0].SourceID != "fake" || !res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	d, chs, err := m.Manga(ctx, res.Mangas[0].MangaRef, true)
	if err != nil || d.Author != "A" || d.Status != source.StatusOngoing || len(chs) != 1 || chs[0].Number != 2 {
		t.Fatalf("manga: %v %+v %+v", err, d, chs)
	}
	got, err := m.Pages(ctx, source.ChapterRef{Manga: d.MangaRef, URL: chs[0].URL})
	if err != nil || len(got) != 1 || got[0].SourceID != "fake" {
		t.Fatalf("pages: %v %+v", err, got)
	}

	// a page can be fetched here, or handed to a worker with the same headers
	body, ct, err := m.FetchPage(ctx, got[0])
	if err != nil || ct != "image/jpeg" {
		t.Fatalf("fetch: %v %q", err, ct)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if string(data) != "jpeg-bytes" {
		t.Fatalf("page bytes %q", data)
	}
	req, err := m.PageRequest(ctx, got[0])
	if err != nil || req.URL != pages[0].URL || req.Headers["Referer"] == "" || req.Headers["User-Agent"] == "" {
		t.Fatalf("page request: %v %+v", err, req)
	}
}

// TestModuleRemembersOptions: a site's own setting is applied and survives a
// restart, since sites keep their state in memory.
func TestModuleRemembersOptions(t *testing.T) {
	m, site := newModule(t, "https://example.org", nil)
	ctx := context.Background()
	prefs, err := m.SourcePreferences(ctx, "fake")
	if err != nil || len(prefs) != 1 || prefs[0].Type != "list" || len(prefs[0].EntryValues) != 2 {
		t.Fatalf("preferences: %v %+v", err, prefs)
	}
	if err := m.SetSourcePreference(ctx, "fake", 0, "list", "de"); err != nil || site.lang != "de" {
		t.Fatalf("set: %v %q", err, site.lang)
	}

	// a new module over the same folder starts where the old one left off
	again := &Module{log: m.log, sites: []sourcekit.Site{&fakeSite{base: "https://example.org", lang: "en"}}, optionsPath: m.optionsPath}
	again.byID = map[string]sourcekit.Site{"fake": again.sites[0]}
	again.loadOptions()
	if got := again.sites[0].Info().Lang; got != "de" {
		t.Fatalf("after restart the language is %q", got)
	}
}

// TestModuleUnknownSite: asking for a site that isn't built in says so.
func TestModuleUnknownSite(t *testing.T) {
	m, _ := newModule(t, "https://example.org", nil)
	if _, err := m.Search(context.Background(), "nope", "x", 1); err == nil {
		t.Fatal("an unknown site should be refused")
	}
}
