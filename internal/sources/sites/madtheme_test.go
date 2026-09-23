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

// The markup below has the shape the theme's pages have (and the extension
// reads): results are .book-detailed-item cards, the manga page's metadata
// is ".meta > p" rows of a <strong> label and links, and the legacy chapter
// endpoint answers with the same #chapter-list the manga page carries.
const (
	madSearchHTML = `
<div class="book-detailed-item"><div class="thumb"><a href="/manga/12345-solo-leveling" title="Solo Leveling">
  <img data-src="/static/covers/12345.jpg" src="/static/placeholder.gif"></a></div>
  <div class="meta"><div class="title"><h3><a href="/manga/12345-solo-leveling">Solo Leveling</a></h3></div>
  <div class="summary">A weak hunter.</div><div class="genres"><span>Action</span></div></div></div>
<div class="book-detailed-item"><div class="thumb"><a href="https://kaliscan.com/manga/678-other" title="Other">
  <img data-src="https://cdn.example/678.jpg"></a></div></div>
<div class="paginator"><a href="?page=1" class="active">1</a><a href="?page=2">2</a><a href="?page=2" rel="next">&raquo;</a></div>`

	madMangaHTML = `
<div class="detail"><div class="name box"><h1>Solo Leveling</h1><h2>Na Honjaman Level Up; Solo Leveling, Only I Level Up</h2></div>
  <div class="meta box">
    <p><strong>Authors :</strong> <a href="/author/chugong"><span>Chugong</span>,</a> <a href="/author/dubu"><span>DUBU</span></a></p>
    <p><strong>Status :</strong> <a href="/status/completed"><span>Completed</span></a></p>
    <p><strong>Genres :</strong> <a href="/genre/action"> Action ,</a> <a href="/genre/fantasy"> Fantasy</a></p>
  </div></div>
<div id="cover"><img data-src="/static/covers/12345.jpg"></div>
<div class="summary"><div class="content">E-class hunter Jinwoo Sung</div><p>is the weakest of them all.</p></div>`

	madChaptersHTML = `
<ul id="chapter-list">
  <li><a href="/manga//12345-solo-leveling/chapter-201"><strong class="chapter-title">Chapter 201</strong>
    <time class="chapter-update">3 days ago</time></a></li>
  <li><a href="/manga/12345-solo-leveling/chapter-200"><strong class="chapter-title">Chapter 200</strong>
    <time class="chapter-update">Mar 5, 2024</time></a></li>
  <li><a href="/manga/12345-solo-leveling/chapter-200"><strong class="chapter-title">Chapter 200</strong></a></li>
  <li><a href="https://elsewhere.example/read/199"><strong class="chapter-title">Chapter 199</strong></a></li>
</ul>`

	madChapterHTML = `<html><body><div id="chapter-images"></div>
<script>var chapterId = 99812; var bookId = 12345;</script></body></html>`

	madServerHTML = `<script>var mainServer = "//cdn.example/"; var chapImages = 'res/1.jpg,res/2.jpg,res/3.jpg';</script>`
)

func newMadTheme(t *testing.T) *madTheme {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("q") != "solo" || q.Get("page") != "1" || q.Get("sort") != "views" || q.Get("status") != "all" {
			t.Errorf("search query %v", q)
		}
		if r.Header.Get("Referer") == "" {
			t.Error("the site expects a referer")
		}
		_, _ = w.Write([]byte(madSearchHTML))
	})
	mux.HandleFunc("/manga/12345-solo-leveling", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(madMangaHTML)) })
	mux.HandleFunc("/service/backend/chaplist/", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("manga_id") != "12345" || q.Get("manga_name") != "Solo Leveling" {
			t.Errorf("chapter list query %v", q)
		}
		_, _ = w.Write([]byte(madChaptersHTML))
	})
	mux.HandleFunc("/manga/12345-solo-leveling/chapter-201", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(madChapterHTML))
	})
	mux.HandleFunc("/service/backend/chapterServer/", func(w http.ResponseWriter, r *http.Request) {
		if q := r.URL.Query(); q.Get("chapter_id") != "99812" || q.Get("server_id") != "1" {
			t.Errorf("chapter server query %v", q)
		}
		_, _ = w.Write([]byte(madServerHTML))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &madTheme{c: sourcekit.NewClient(srv.Client()), id: "test", name: "KaliScan", base: srv.URL, legacyAPI: true,
		gate: &madGate{}}
}

// TestMadTheme reads a series the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestMadTheme(t *testing.T) {
	m := newMadTheme(t)
	ctx := context.Background()

	res, err := m.Search(ctx, "solo", 1)
	if err != nil || len(res.Mangas) != 2 || !res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	got := res.Mangas[0]
	if got.Title != "Solo Leveling" || got.URL != "/manga/12345-solo-leveling" || got.CoverURL != m.base+"/static/covers/12345.jpg" {
		t.Fatalf("result %+v", got)
	}
	// a result with a full address is kept as a path
	if res.Mangas[1].URL != "/manga/678-other" {
		t.Fatalf("result %+v", res.Mangas[1])
	}

	d, err := m.Details(ctx, sourcekit.Ref{URL: got.URL})
	if err != nil || d.Title != "Solo Leveling" || d.Author != "Chugong, DUBU" || d.Status != sourcekit.StatusCompleted ||
		strings.Join(d.Genres, "|") != "Action|Fantasy" {
		t.Fatalf("details: %v %+v", err, d)
	}
	if d.Description != "E-class hunter Jinwoo Sung is the weakest of them all.\n\nAlt name(s): Na Honjaman Level Up, Only I Level Up" {
		t.Fatalf("description %q", d.Description)
	}

	// the stored title is what the legacy endpoint wants alongside the id
	chs, err := m.Chapters(ctx, sourcekit.Ref{URL: got.URL, Title: "Solo Leveling"})
	if err != nil || len(chs) != 3 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	// repeated slashes are folded, and a chapter hosted elsewhere keeps its
	// full address
	if chs[2].URL != "https://elsewhere.example/read/199" {
		t.Fatalf("external chapter %+v", chs[2])
	}
	if chs[0].URL != "/manga/12345-solo-leveling/chapter-201" || chs[0].Number != 201 || chs[0].UploadedAt == nil ||
		time.Since(*chs[0].UploadedAt) < 71*time.Hour {
		t.Fatalf("chapter %+v", chs[0])
	}
	if at := chs[1].UploadedAt; at == nil || at.Format("2006-01-02") != "2024-03-05" {
		t.Fatalf("chapter date %+v", chs[1])
	}

	pages, err := m.Pages(ctx, sourcekit.PageRef{URL: chs[0].URL})
	if err != nil || len(pages) != 3 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if pages[0].URL != "https://cdn.example/res/1.jpg" || pages[2].Index != 2 || pages[0].Headers["Referer"] == "" {
		t.Fatalf("pages %+v", pages)
	}
}

// TestMadThemeChaptersFromPage: without an id in the link the chapters come
// off the manga page, and when the page shows only the newest few, the rest
// come from the API, merged where the two lists meet.
func TestMadThemeChaptersFromPage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/manga/some-slug", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<ul id="chapter-list">
		  <li><a href="/some-slug/chapter-3"><span class="chapter-title">Chapter 3</span></a></li>
		  <li><a href="/some-slug/chapter-2"><span class="chapter-title">Chapter 2</span></a></li></ul>
		<div id="show-more-chapters"><span onclick="getChapters()">Show all</span></div>
		<script>var bookId = 777; var bookSlug = "some-slug";</script>`))
	})
	mux.HandleFunc("/api/manga/777/chapters", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("source") != "detail" {
			t.Errorf("chapter api query %v", r.URL.Query())
		}
		_, _ = w.Write([]byte(`<ul id="chapter-list">
		  <li><a href="/some-slug/chapter-2"><span class="chapter-title">Chapter 2</span></a></li>
		  <li><a href="/some-slug/chapter-1"><span class="chapter-title">Chapter 1</span></a></li></ul>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	m := &madTheme{c: sourcekit.NewClient(srv.Client()), base: srv.URL, gate: &madGate{}}
	chs, err := m.Chapters(context.Background(), sourcekit.Ref{URL: "/manga/some-slug"})
	if err != nil || len(chs) != 3 || chs[0].Number != 3 || chs[2].URL != "/some-slug/chapter-1" {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
}

// TestMadThemePageImages: images come from tags unless the script lists more
// (a site lazy-loading all but the first few), and the known-broken s20
// server is skipped for the tag's own fallback.
func TestMadThemePageImages(t *testing.T) {
	loc := "https://kaliscan.com/manga/x/chapter-1"
	tags := `<div id="chapter-images">
	  <img data-src="https://s20.cdn.example/toonily/manga/1.jpg" onerror="this.onerror=null;this.src='//sb.cdn.example/manga/1.jpg?v=1'">
	  <img data-src="/res/2.jpg"></div>`
	got, err := madPageURLs(tags, loc)
	if err != nil || strings.Join(got, " ") != "https://sb.cdn.example/manga/1.jpg?v=1 https://kaliscan.com/res/2.jpg" {
		t.Fatalf("tags: %v %v", err, got)
	}
	lazy := tags + `<script>var chapImages = 'https://a.example/1.jpg,https://a.example/2.jpg,https://a.example/3.jpg'</script>`
	if got, _ := madPageURLs(lazy, loc); len(got) != 3 || got[2] != "https://a.example/3.jpg" {
		t.Fatalf("script: %v", got)
	}
	// paths without a host can't be used: the tags win
	relative := tags + `<script>var chapImages = '/1.jpg,/2.jpg,/3.jpg'</script>`
	if got, _ := madPageURLs(relative, loc); len(got) != 2 {
		t.Fatalf("relative script: %v", got)
	}
}

// TestMadDate reads both the absolute and the relative dates the theme shows.
func TestMadDate(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]string{
		"Sep 03, 2024": "2024-09-03T00:00:00Z", "Mar 5, 2024": "2024-03-05T00:00:00Z",
		"2 hours ago": "2026-09-23T10:00:00Z", "1 day ago": "2026-09-22T12:00:00Z",
		"3 months ago": "2026-06-23T12:00:00Z", "1 year ago": "2025-09-23T12:00:00Z",
	} {
		got := madDate(in, now)
		if got == nil || got.Format(time.RFC3339) != want {
			t.Errorf("madDate(%q) = %v, want %s", in, got, want)
		}
	}
	if madDate("yesterday-ish", now) != nil {
		t.Error("an unknown date should be left out")
	}
}
