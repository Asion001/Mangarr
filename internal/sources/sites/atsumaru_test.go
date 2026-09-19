package sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The answers below are trimmed from the site's own. Note that its search
// index writes image paths with the "/static/" prefix and its browse
// endpoints without it.
const (
	atSearchJSON = `{"found":41,"hits":[
	  {"document":{"id":"CM0wz","title":"BERSERK","englishTitle":"Berserk","chapterCount":403,
	    "poster":"/static/posters/abc.jpg"}},
	  {"document":{"id":"Bb7s6","title":"Berserk of Gluttony","poster":"/static/posters/def.jpg"}}]}`

	atBrowseJSON = `{"items":[{"id":"XnorR","title":"Myst, Might, Mayhem","image":"posters/ghi.jpg"}]}`

	atPageJSON = `{"mangaPage":{"id":"CM0wz","title":"BERSERK","englishTitle":"Berserk","status":"Ongoing",
	  "synopsis":"His name is Guts.","poster":{"image":"posters/abc.jpg"},
	  "authors":[{"name":"MIURA Kentaro","type":"Author"},{"name":"Studio Gaga","type":"Artist"}],
	  "genres":[{"name":"Action"},{"name":"Fantasy"}],
	  "scanlators":[{"id":"team-1","name":"Alpha"}],"totalChapterCount":37.5}}`

	atChaptersJSON = `{"chapters":[
	  {"id":"KrQuF","scanlationMangaId":"team-1","title":"Chapter 386","number":386,"createdAt":1783633137519},
	  {"id":"E5PXRSUC","scanlationMangaId":"team-1","title":"Prologue 1","number":1,"createdAt":1752583810864}]}`

	atReadJSON = `{"readChapter":{"id":"KrQuF","pages":[
	  {"image":"/static/pages/team-1/KrQuF/0.webp"},{"image":"pages/team-1/KrQuF/1.webp"}]}}`
)

func newAtsumaru(t *testing.T) *atsumaru {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/collections/manga/documents/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("q") != "berserk" || q.Get("query_by") == "" {
			t.Errorf("search query %v", q)
		}
		if !strings.Contains(q.Get("filter_by"), "isAdult:=false") {
			t.Errorf("adult titles are not filtered out: %q", q.Get("filter_by"))
		}
		_, _ = w.Write([]byte(atSearchJSON))
	})
	mux.HandleFunc("/api/home2/popular", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("timeframe") != "daily" {
			t.Errorf("timeframe %q", r.URL.Query().Get("timeframe"))
		}
		_, _ = w.Write([]byte(atBrowseJSON))
	})
	mux.HandleFunc("/api/manga/page", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "CM0wz" {
			t.Errorf("manga id %q", r.URL.Query().Get("id"))
		}
		_, _ = w.Write([]byte(atPageJSON))
	})
	mux.HandleFunc("/api/manga/allChapters", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mangaId") != "CM0wz" {
			t.Errorf("manga id %q", r.URL.Query().Get("mangaId"))
		}
		_, _ = w.Write([]byte(atChaptersJSON))
	})
	mux.HandleFunc("/api/read/chapter", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("mangaId") != "CM0wz" || q.Get("chapterId") != "KrQuF" {
			t.Errorf("read %v", q)
		}
		_, _ = w.Write([]byte(atReadJSON))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &atsumaru{c: sourcekit.NewClient(srv.Client()), site: srv.URL, cdn: "https://cdn.example"}
}

// TestAtsumaru reads a series the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestAtsumaru(t *testing.T) {
	a := newAtsumaru(t)
	ctx := context.Background()

	res, err := a.Search(ctx, "berserk", 1)
	if err != nil || len(res.Mangas) != 2 || !res.HasNext { // 41 found, 40 to a page
		t.Fatalf("search: %v %+v", err, res)
	}
	got := res.Mangas[0]
	if got.Title != "Berserk" || got.URL != "/manga/CM0wz" || got.ID != "CM0wz" ||
		got.CoverURL != "https://cdn.example/static/posters/abc.jpg" || got.Chapters == nil || *got.Chapters != 403 {
		t.Fatalf("result %+v", got)
	}

	pop, err := a.Popular(ctx, 1)
	if err != nil || len(pop.Mangas) != 1 || pop.Mangas[0].CoverURL != "https://cdn.example/static/posters/ghi.jpg" {
		t.Fatalf("popular: %v %+v", err, pop)
	}

	d, err := a.Details(ctx, sourcekit.Ref{URL: got.URL, ID: got.ID})
	if err != nil || d.Status != sourcekit.StatusOngoing || d.Author != "MIURA Kentaro" || d.Artist != "Studio Gaga" ||
		len(d.Genres) != 2 || d.Description != "His name is Guts." || d.Chapters != nil {
		t.Fatalf("details: %v %+v", err, d)
	}

	chs, err := a.Chapters(ctx, sourcekit.Ref{URL: d.URL, ID: d.ID})
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if chs[0].Number != 386 || chs[0].URL != "/read/CM0wz/KrQuF" || chs[0].Scanlator != "Alpha" || chs[0].UploadedAt == nil {
		t.Fatalf("chapter %+v", chs[0])
	}

	// the chapter url carries both ids, so pages need nothing else remembered
	pages, err := a.Pages(ctx, sourcekit.PageRef{URL: chs[0].URL})
	if err != nil || len(pages) != 2 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if pages[0].URL != "https://cdn.example/static/pages/team-1/KrQuF/0.webp" ||
		pages[1].URL != "https://cdn.example/static/pages/team-1/KrQuF/1.webp" {
		t.Fatalf("pages %+v", pages)
	}
	if pages[0].Headers["Referer"] == "" {
		t.Fatalf("page headers %+v", pages[0].Headers)
	}
}
