package sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The markup below is trimmed to what the extension reads: the manga page's
// "#comicDetail" block and the chapter page's script with its image list.
const mballDetailHTML = `<html><head><meta name="csrf-token" content="%TOKEN%"></head><body>
<div id="featuredComicsCarousel"><img src="/images/flags/jp.png"></div>
<div id="comicDetail">
  <h6>Berserk <small>2 chapters</small></h6>
  <span class="badge">Published: 1989</span>
  <span data-tag-id="1">Action</span><span data-tag-id="2">Dark Fantasy</span>
  <span data-person-id="9">Kentaro Miura</span>
  <span class="badge-status">Ongoing</span>
</div>
<img class="featured-cover" src="/covers/berserk.jpg">
<div id="descriptionContent"><p>Guts, the Black Swordsman.</p></div>
</body></html>`

const mballReaderHTML = `<html><head><meta name="csrf-token" content="%TOKEN%"></head><body>
<script>const titleId = ` + "`68514a`" + `; const chapterId = ` + "`ch-en`" + `;</script>
<script>const chapterImages = JSON.parse(` + "`" + `["https://cdn.example/1.jpg","https:\/\/cdn.example\/2.jpg"]` + "`" + `);</script>
</body></html>`

func newMangaBall(t *testing.T) (*mangaball, *int) {
	t.Helper()
	var mu sync.Mutex
	tokens := 0 // the home page hands out a new token each time
	current := func() string { return "tok" + strconv.Itoa(tokens) }
	stale := true
	mux := http.NewServeMux()
	page := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			_, _ = w.Write([]byte(strings.ReplaceAll(body, "%TOKEN%", current())))
		}
	}
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tokens++
		mu.Unlock()
		page(`<meta name="csrf-token" content="%TOKEN%">`)(w, r)
	})
	// api checks what the site's API wants of every call, and turns the
	// first one away as if its token had expired
	api := func(check func(r *http.Request), body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			ok := r.Header.Get("X-CSRF-TOKEN") == current() && !stale
			stale = false
			mu.Unlock()
			if !ok {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			if r.Method != http.MethodPost || r.Header.Get("X-Requested-With") != "XMLHttpRequest" {
				t.Errorf("%s %s: not an API call", r.Method, r.URL.Path)
			}
			if c, err := r.Cookie("show18PlusContent"); err != nil || c.Value != "true" {
				t.Errorf("18+ cookie: %v %v", c, err)
			}
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			check(r)
			_, _ = w.Write([]byte(body))
		}
	}
	mux.HandleFunc("/api/v1/smart-search/search/", api(func(r *http.Request) {
		if r.PostForm.Get("search_input") != "berserk" {
			t.Errorf("quick search %v", r.PostForm)
		}
	}, `{"data":{"manga":[{"title":"Berserk","img":"https://img.example/b.jpg","url":"/title-detail/berserk-68514a/"}]}}`))
	mux.HandleFunc("/api/v1/title/search-advanced/", api(func(r *http.Request) {
		f := r.PostForm
		if f.Get("search_input") != "berserk" || f.Get("filters[page]") != "1" || f.Get("filters[sort]") != "updated_chapters_desc" ||
			strings.Join(f["filters[translatedLanguage][]"], ",") != "en" || f.Get("filters[contentRating]") != "any" {
			t.Errorf("advanced search %v", f)
		}
	}, `{"data":[{"url":"https://mangaball.net/title-detail/berserk-deluxe-6a0000/","name":"Berserk Deluxe","cover":"https://img.example/d.jpg"}],
		"pagination":{"current_page":1,"last_page":1}}`))
	mux.HandleFunc("/api/v1/chapter/chapter-listing-by-title-id/", api(func(r *http.Request) {
		if r.PostForm.Get("title_id") != "68514a" {
			t.Errorf("chapters of %q", r.PostForm.Get("title_id"))
		}
	}, `{"ALL_CHAPTERS":[
		{"number_float":2,"translations":[
			{"id":"ch-en","name":"Chapter 2 The Brand","language":"en","group":{"_id":"6851aaaaaaaaaaaaaaaaaaaa","name":"Evil Genius"},"date":"2025-06-01 12:30:00","volume":1},
			{"id":"ch-es","name":"La marca","language":"es","group":{"_id":"x","name":"Otro"},"date":"2025-06-01 12:30:00","volume":0}]},
		{"number_float":1.5,"translations":[
			{"id":"ch-en-1","name":"Extra","language":"en","group":{"_id":"mangadex","name":"Some Group"},"date":"bad","volume":0}]}]}`))
	mux.HandleFunc("/title-detail/berserk-68514a/", page(mballDetailHTML))
	mux.HandleFunc("/chapter-detail/ch-en/", page(mballReaderHTML))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &mangaball{c: sourcekit.NewClient(srv.Client()), base: srv.URL, lang: "en", adult: true}, &tokens
}

// TestMangaBall walks a series the way the library does: find it, read its
// details, list chapters, then a chapter's pages. The first API call finds
// its token expired, and has to fetch a new one.
func TestMangaBall(t *testing.T) {
	m, tokens := newMangaBall(t)
	ctx := context.Background()

	res, err := m.Search(ctx, "berserk", 1)
	if err != nil || len(res.Mangas) != 1 || !res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	if got := res.Mangas[0]; got.URL != "berserk-68514a" || got.Title != "Berserk" || got.CoverURL != "https://img.example/b.jpg" {
		t.Fatalf("result %+v", got)
	}
	if *tokens != 2 {
		t.Fatalf("the stale token should have been fetched again (%d fetches)", *tokens)
	}
	more, err := m.Search(ctx, "berserk", 2)
	if err != nil || len(more.Mangas) != 1 || more.Mangas[0].URL != "berserk-deluxe-6a0000" || more.HasNext {
		t.Fatalf("second page: %v %+v", err, more)
	}

	d, err := m.Details(ctx, sourcekit.Ref{URL: "berserk-68514a"})
	if err != nil || d.Title != "Berserk" || d.Author != "Kentaro Miura" || d.Status != sourcekit.StatusOngoing ||
		d.Description != "Guts, the Black Swordsman." || d.CoverURL != m.base+"/covers/berserk.jpg" ||
		strings.Join(d.Genres, ",") != "Manga,Action,Dark Fantasy" {
		t.Fatalf("details: %v %+v", err, d)
	}

	chs, err := m.Chapters(ctx, sourcekit.Ref{URL: "berserk-68514a"})
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if c := chs[0]; c.URL != "ch-en" || c.Name != "Vol. 1 Chapter 2 The Brand" || c.Number != 2 || c.Scanlator != "Evil Genius" ||
		c.UploadedAt == nil || c.UploadedAt.Hour() != 12 {
		t.Fatalf("first chapter %+v", c)
	}
	// a group id that isn't the site's own names where the chapter came from
	if c := chs[1]; c.Name != "Ch. 1.5 Extra" || c.Scanlator != "Some Group (mangadex)" || c.UploadedAt != nil {
		t.Fatalf("second chapter %+v", c)
	}

	pages, err := m.Pages(ctx, sourcekit.PageRef{URL: chs[0].URL})
	if err != nil || len(pages) != 2 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if pages[0].URL != "https://cdn.example/1.jpg" || pages[1].URL != "https://cdn.example/2.jpg" || pages[0].Headers["Referer"] == "" {
		t.Fatalf("pages %+v", pages)
	}
}

// TestMangaBallLanguages: each language is a catalog with Keiyoushi's id,
// asking the site for its regional variants.
func TestMangaBallLanguages(t *testing.T) {
	n := 0
	for _, s := range sourcekit.Build(sourcekit.Deps{Client: sourcekit.NewClient(nil)}) {
		if s.Info().Name == "Manga Ball" {
			n++
		}
	}
	if n != len(mballLangs) {
		t.Fatalf("%d Manga Ball catalogs for %d languages", n, len(mballLangs))
	}
	if got := (&mangaball{lang: "ja"}).siteLangs(); strings.Join(got, ",") != "jp" {
		t.Fatalf("ja asks the site for %v", got)
	}
	if got := (&mangaball{lang: "pt-BR"}).siteLangs(); strings.Join(got, ",") != "pt-br,pt-pt" {
		t.Fatalf("pt-BR asks the site for %v", got)
	}
	if mballSlug("https://mangaball.net/title-detail/berserk-68514a/") != "berserk-68514a" || mballSlug("berserk-68514a") != "berserk-68514a" {
		t.Fatal("slug")
	}
}
