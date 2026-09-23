package sites

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The markup below is shaped like what the Keiyoushi extension parses: search
// results are cards, a manga page holds the first chapters and a pager whose
// links call load_list_chapter(n), later chapter pages come as JSON holding
// HTML, and the reader packs its image list in a token.
const (
	lmangaSearchHTML = `<div class="card-body">
	  <div class="card"><a href="https://likemanga.ink/martial-peak-12/"><img data-src="/covers/mp.jpg"></a>
	    <p class="title-manga">Martial Peak</p></div>
	  <div class="card"><a href="/other-13/"><img srcset="https://cdn.example/o.jpg 1x, https://cdn.example/o2.jpg 2x"></a>
	    <p class="title-manga">Other</p></div></div>
	  <ul class="pagination"><li><a href="?pageNum=2">»</a></li></ul>`

	lmangaMangaHTML = `<div class="detail-info"><img data-cfsrc="https://cdn.example/mp-big.jpg"></div>
	  <h1 id="title-detail-manga" data-manga="12">Martial Peak</h1>
	  <ul class="list-info">
	    <li class="author"><p>Author</p><p>Momo</p></li>
	    <li class="status"><p>Status</p><p>In process</p></li>
	    <li><a href="/genres/action/">Action</a><a href="/genres/martial-arts/">Martial Arts</a></li></ul>
	  <div id="summary_shortened">The journey to the martial peak is lonely.</div>
	  <ul><li class="wp-manga-chapter"><a href="/martial-peak-12/chapter-3800-100/">Chapter 3800</a>
	    <span class="chapter-release-date">September 3, 2026</span></li></ul>
	  <div class="chapters_pagination"><a onclick="load_list_chapter(1)">1</a><a onclick="load_list_chapter(2)">2</a>
	    <a class="next" onclick="load_list_chapter(2)">»</a></div>`

	lmangaAjaxHTML = `<li class="wp-manga-chapter"><a href="/martial-peak-12/chapter-3799-99/">Chapter 3799</a>
	  <span class="chapter-release-date">August 27, 2026</span></li>`

	lmangaReaderHTML = `<div class="reading"><input id="currentlink" value="https://img.example/manga">
	  <input id="next_img_token" value="eyJhbGciOiJIUzI1NiJ9.eyJkYXRhIjogIld5SmphREV2TURBeExtcHdaeUlzSUNKamFERXZNREF5TG1wd1p5SmQifQ.sig"></div>`

	lmangaReaderPlainHTML = `<div class="reading-detail box_doc"><img src="https://img.example/a.jpg">
	  <noscript><img src="https://img.example/a.jpg"></noscript><img data-src="https://img.example/b.jpg"></div>`
)

func newLikeManga(t *testing.T) *likemanga {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		switch q.Get("act") {
		case "searchadvance":
			if q.Get("f[keyword]") != "martial" || q.Get("pageNum") != "" {
				t.Errorf("search %v", q)
			}
			_, _ = w.Write([]byte(lmangaSearchHTML))
		case "ajax":
			if q.Get("code") != "load_list_chapter" || q.Get("manga_id") != "12" || q.Get("page_num") != "2" || q.Get("chap_id") != "0" {
				t.Errorf("ajax %v", q)
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"list_chap": lmangaAjaxHTML})
		default:
			t.Errorf("unexpected %v", q)
		}
	})
	mux.HandleFunc("/martial-peak-12/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			t.Error("the site expects a referer")
		}
		_, _ = w.Write([]byte(lmangaMangaHTML))
	})
	mux.HandleFunc("/martial-peak-12/chapter-3800-100/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(lmangaReaderHTML)) })
	mux.HandleFunc("/martial-peak-12/chapter-3799-99/", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(lmangaReaderPlainHTML)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &likemanga{c: sourcekit.NewClient(srv.Client()), base: srv.URL}
}

// TestLikeManga reads a manga the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestLikeManga(t *testing.T) {
	l := newLikeManga(t)
	ctx := context.Background()

	res, err := l.Search(ctx, "martial", 1)
	if err != nil || len(res.Mangas) != 2 || !res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	if r := res.Mangas[0]; r.URL != "/martial-peak-12/" || r.Title != "Martial Peak" || !strings.HasSuffix(r.CoverURL, "/covers/mp.jpg") {
		t.Fatalf("result %+v", r)
	}
	if res.Mangas[1].CoverURL != "https://cdn.example/o.jpg" {
		t.Fatalf("srcset cover %q", res.Mangas[1].CoverURL)
	}

	ref := sourcekit.Ref{URL: res.Mangas[0].URL}
	d, err := l.Details(ctx, ref)
	if err != nil || d.Title != "Martial Peak" || d.Author != "Momo" || d.Status != sourcekit.StatusOngoing ||
		strings.Join(d.Genres, ",") != "Action,Martial Arts" || d.CoverURL != "https://cdn.example/mp-big.jpg" ||
		d.Description != "The journey to the martial peak is lonely." {
		t.Fatalf("details: %v %+v", err, d)
	}

	// the first page is on the manga's page, the second from the ajax endpoint
	chs, err := l.Chapters(ctx, ref)
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if c := chs[0]; c.URL != "/martial-peak-12/chapter-3800-100/" || c.Number != 3800 || c.UploadedAt == nil ||
		c.UploadedAt.Format("2006-01-02") != "2026-09-03" {
		t.Fatalf("chapter %+v", c)
	}
	if c := chs[1]; c.Name != "Chapter 3799" || c.UploadedAt == nil {
		t.Fatalf("chapter %+v", c)
	}

	pages, err := l.Pages(ctx, sourcekit.PageRef{URL: chs[0].URL})
	if err != nil || len(pages) != 2 || pages[1].URL != "https://img.example/manga/ch1/002.jpg" || pages[0].Headers["Referer"] == "" {
		t.Fatalf("token pages: %v %+v", err, pages)
	}
	// without a token the images are plain, and noscript copies are skipped
	pages, err = l.Pages(ctx, sourcekit.PageRef{URL: chs[1].URL})
	if err != nil || len(pages) != 2 || pages[1].URL != "https://img.example/b.jpg" {
		t.Fatalf("plain pages: %v %+v", err, pages)
	}
}
