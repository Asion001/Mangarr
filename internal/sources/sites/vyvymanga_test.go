package sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The markup below is shaped like what the Keiyoushi extension parses: search
// results are .comic-item cards, a manga page labels its fields with
// .pre-title and lists chapters as links, and a chapter's page holds lazy
// images.
const (
	vyvySearchHTML = `<div class="comic-item"><a href="/manga/the-beginning-after-the-end">
	    <div class="comic-image"><img class="image lozad" data-src="https://cdn.example/tbate.jpg"></div>
	    <div class="comic-title">The Beginning After the End</div></a></div>
	  <ul class="pagination"><li><a rel="next" href="/search?page=2">»</a></li></ul>`

	vyvyMangaHTML = `<div class="img-manga"><img src="https://cdn.example/tbate-big.jpg"></div>
	  <h1> The Beginning After the End </h1>
	  <div><span class="pre-title">Author(s):</span> <a href="/author/1">TurtleMe</a></div>
	  <div><span class="pre-title">Artist(s):</span> <a href="/artist/2">Fuyuki23</a></div>
	  <div><span class="pre-title">Status:</span> <span class="space"> </span><span>Ongoing</span></div>
	  <div><span class="pre-title">Genres:</span> <a href="/genre/1">Action</a>, <a href="/genre/2">Fantasy</a></div>
	  <div class="summary"><div class="content">King Grey has unrivaled strength.</div></div>
	  <div class="list-group">
	    <a href="https://mangavyvy.net/chapter/abc?t=1"><span>Chapter 175</span><p>3 hours ago</p></a>
	    <a href="https://mangavyvy.net/chapter/def?t=1"><span>Chapter 174</span><p>Jan 05, 2024</p></a>
	  </div>`

	vyvyReaderHTML = `<img class="d-block" data-src="https://img.example/1.jpg"><img class="d-block" data-src="https://img.example/2.jpg">`
)

func newVyvyManga(t *testing.T, now time.Time, chapterLink string) *vyvymanga {
	t.Helper()
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("q") != "beginning" || q.Get("page") != "1" || q.Get("completed") != "2" || q.Get("sort") != "viewed" {
			t.Errorf("search %v", q)
		}
		_, _ = w.Write([]byte(vyvySearchHTML))
	})
	mux.HandleFunc("/manga/the-beginning-after-the-end", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.ReplaceAll(vyvyMangaHTML, "https://mangavyvy.net/chapter/abc?t=1", base+chapterLink)))
	})
	mux.HandleFunc("/chapter/abc", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "2" {
			http.NotFound(w, r) // the old link has gone stale
			return
		}
		_, _ = w.Write([]byte(vyvyReaderHTML))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	return &vyvymanga{c: sourcekit.NewClient(srv.Client()), base: srv.URL, now: func() time.Time { return now }}
}

// TestVyvyManga reads a manga the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestVyvyManga(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	v := newVyvyManga(t, now, "/chapter/abc?t=2")
	ctx := context.Background()

	res, err := v.Search(ctx, "beginning", 1)
	if err != nil || len(res.Mangas) != 1 || !res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	ref := sourcekit.Ref{URL: res.Mangas[0].URL}
	if ref.URL != "/manga/the-beginning-after-the-end" || res.Mangas[0].CoverURL != "https://cdn.example/tbate.jpg" {
		t.Fatalf("result %+v", res.Mangas[0])
	}

	d, err := v.Details(ctx, ref)
	if err != nil || d.Title != "The Beginning After the End" || d.Author != "TurtleMe" || d.Artist != "Fuyuki23" ||
		d.Status != sourcekit.StatusOngoing || strings.Join(d.Genres, ",") != "Action,Fantasy" ||
		d.Description != "King Grey has unrivaled strength." || d.CoverURL != "https://cdn.example/tbate-big.jpg" {
		t.Fatalf("details: %v %+v", err, d)
	}

	chs, err := v.Chapters(ctx, ref)
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	c := chs[0]
	if c.URL != vyvymangaKey(time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC).UnixMilli(), "Chapter 175") ||
		c.Number != 175 || c.UploadedAt == nil || !c.UploadedAt.Equal(now.Add(-3*time.Hour)) || !strings.HasSuffix(c.ID, "/chapter/abc?t=2") {
		t.Fatalf("chapter %+v", c)
	}
	// a written-out date gives the extension's own key
	if chs[1].URL != vyvymangaKey(1704412800000, "Chapter 174") || chs[1].UploadedAt == nil {
		t.Fatalf("chapter %+v", chs[1])
	}

	pages, err := v.Pages(ctx, sourcekit.PageRef{URL: c.URL, ID: c.ID, Manga: ref})
	if err != nil || len(pages) != 2 || pages[1].URL != "https://img.example/2.jpg" {
		t.Fatalf("pages: %v %+v", err, pages)
	}
}

// TestVyvyMangaStaleLink: a chapter's link changes over time; its pages are
// still found through the manga's page.
func TestVyvyMangaStaleLink(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	v := newVyvyManga(t, now, "/chapter/abc?t=2")
	ref := sourcekit.Ref{URL: "/manga/the-beginning-after-the-end"}
	key := vyvymangaKey(time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC).UnixMilli(), "Chapter 175")
	for _, id := range []string{v.base + "/chapter/abc?t=1", ""} {
		pages, err := v.Pages(context.Background(), sourcekit.PageRef{URL: key, ID: id, Manga: ref})
		if err != nil || len(pages) != 2 {
			t.Fatalf("pages with id %q: %v %+v", id, err, pages)
		}
	}
}

// TestVyvyMangaKey matches the extension's md5 of "<ms>:<title>".
func TestVyvyMangaKey(t *testing.T) {
	// md5("1704412800000:Chapter 174") = 29c2610305448c4704520c8831247fa6
	if got := vyvymangaKey(1704412800000, "Chapter 174"); got != "8831247fa6" {
		t.Fatalf("key %q", got)
	}
	if _, ms := vyvymangaDate("5 weeks ago", time.Now()); ms != 0 {
		t.Fatal("the extension only knows days, hours, minutes and seconds")
	}
	if _, ms := vyvymangaDate("Jan 5, 2024", time.Now()); ms != 0 {
		t.Fatal("the extension wants a two-digit day")
	}
}
