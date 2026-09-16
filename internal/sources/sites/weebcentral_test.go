package sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The markup below is trimmed from the site's own responses: a result is two
// links to the same series (the cover, then the title), the metadata is a
// list of "<strong>Label:</strong> <a>value</a>" rows, and chapters and pages
// come from their own endpoints.
const wcSearchHTML = `
<article class="bg-base-300 flex gap-4 p-4"><section>
  <a href="https://weebcentral.com/series/01ABC/Berserk">
    <article><picture><img src="https://covers.example/01ABC.jpg" alt="Berserk cover"></picture></article>
    <div>Official Translation</div>
  </a>
  <div><span><a href="https://weebcentral.com/series/01ABC/Berserk" class="line-clamp-1 link">Berserk</a></span></div>
</section></article>
<article class="bg-base-300 flex gap-4 p-4"><section>
  <a href="https://weebcentral.com/series/01DEF/Other"><article><picture><img src="https://covers.example/01DEF.jpg" alt="Other cover"></picture></article></a>
  <div><span><a href="https://weebcentral.com/series/01DEF/Other" class="line-clamp-1 link">Other Manga</a></span></div>
</section></article>`

const wcSeriesHTML = `
<h1 class="text-2xl font-bold">Berserk</h1>
<img src="https://covers.example/01ABC.jpg" alt="Berserk cover" width="400" height="600">
<ul>
  <li><strong>Description</strong><p class="whitespace-pre-wrap">His name is Guts.</p></li>
  <li><strong>Author(s): </strong><a href="/search?author=1">MIURA Kentaro</a>, <a href="/search?author=2">Studio Gaga</a></li>
  <li><strong>Tags(s): </strong><a href="/search?tag=1">Action</a>, <a href="/search?tag=2">Horror</a></li>
  <li><strong>Status: </strong><a href="/search?status=Ongoing">Ongoing</a></li>
</ul>`

const wcChaptersHTML = `
<div><a href="/chapters/01CH2" class="flex-1"><span class="me-2"><img src="/badge.svg"></span>
  <span class="grow flex items-center gap-2"><span class="">Chapter 386</span></span>
  <time datetime="2026-07-09T21:10:35.950Z">July 9</time></a></div>
<div><a href="/chapters/01CH1" class="flex-1"><span class="grow"><span class="">Chapter 385</span></span>
  <time datetime="2026-06-25T15:46:45.598Z">June 25</time></a></div>`

const wcPagesHTML = `
<section id="chapter-images">
  <img src="https://scans.example/manga/Berserk/0386-001.png" alt="Page 1">
  <img src="https://scans.example/manga/Berserk/0386-002.png" alt="Page 2">
</section>`

func newWeebCentral(t *testing.T) *weebcentral {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/search/data", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("display_mode") != "Full Display" {
			t.Errorf("display mode %q", r.URL.Query().Get("display_mode"))
		}
		_, _ = w.Write([]byte(wcSearchHTML))
	})
	mux.HandleFunc("/series/01ABC", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(wcSeriesHTML)) })
	mux.HandleFunc("/series/01ABC/full-chapter-list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			t.Error("the site expects a referer")
		}
		_, _ = w.Write([]byte(wcChaptersHTML))
	})
	mux.HandleFunc("/chapters/01CH2/images", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("reading_style") != "long_strip" {
			t.Errorf("reading style %q", r.URL.Query().Get("reading_style"))
		}
		_, _ = w.Write([]byte(wcPagesHTML))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &weebcentral{c: sourcekit.NewClient(srv.Client()), base: srv.URL}
}

// TestWeebCentral reads a series the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestWeebCentral(t *testing.T) {
	w := newWeebCentral(t)
	ctx := context.Background()

	res, err := w.Search(ctx, "berserk", 1)
	if err != nil || len(res.Mangas) != 2 {
		t.Fatalf("search: %v %+v", err, res)
	}
	// the cover's link carries badges; the title comes from the other one
	if res.Mangas[0].Title != "Berserk" || res.Mangas[0].URL != "/series/01ABC" || res.Mangas[0].CoverURL == "" {
		t.Fatalf("result %+v", res.Mangas[0])
	}

	d, err := w.Details(ctx, sourcekit.Ref{URL: "/series/01ABC"})
	if err != nil || d.Title != "Berserk" || d.Author != "MIURA Kentaro, Studio Gaga" ||
		d.Status != sourcekit.StatusOngoing || len(d.Genres) != 2 || d.Description != "His name is Guts." {
		t.Fatalf("details: %v %+v", err, d)
	}

	chs, err := w.Chapters(ctx, sourcekit.Ref{URL: "/series/01ABC"})
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if chs[0].Name != "Chapter 386" || chs[0].Number != 386 || chs[0].URL != "/chapters/01CH2" || chs[0].UploadedAt == nil {
		t.Fatalf("chapter %+v", chs[0])
	}

	pages, err := w.Pages(ctx, "/chapters/01CH2")
	if err != nil || len(pages) != 2 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if pages[0].URL != "https://scans.example/manga/Berserk/0386-001.png" || pages[0].Headers["Referer"] == "" {
		t.Fatalf("page %+v", pages[0])
	}
}

// TestWeebCentralIdentity: the slug after a series id changes with the title,
// so the identity mangarr stores must not include it.
func TestWeebCentralIdentity(t *testing.T) {
	for _, in := range []string{"https://weebcentral.com/series/01ABC/Berserk", "/series/01ABC/Some-New-Title", "/series/01ABC"} {
		if got := seriesPath(in); got != "/series/01ABC" {
			t.Fatalf("seriesPath(%q) = %q", in, got)
		}
	}
	if got := seriesPath("/chapters/01CH2"); got != "" {
		t.Fatalf("a chapter url is not a series: %q", got)
	}
}

// TestChapterNumber reads the number the way the site writes it.
func TestChapterNumber(t *testing.T) {
	for name, want := range map[string]float64{
		"Chapter 386": 386, "Chapter 83.5": 83.5, "Ch. 12": 12, "Oneshot": -1, "Volume 3 Chapter 7": 7,
	} {
		if got := chapterNumber(name); got != want {
			t.Fatalf("chapterNumber(%q) = %v, want %v", name, got, want)
		}
	}
}
