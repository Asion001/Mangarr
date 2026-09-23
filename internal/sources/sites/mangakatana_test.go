package sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The markup below is shaped like what the Keiyoushi extension parses: a
// listing of div.item cards, a manga page holding its chapter table, and a
// reader script that names the array its images come from.
const (
	mkatListHTML = `<div id="book_list">
	  <div class="item"><div class="media"><div class="wrap_img"><a href="/manga/one-piece.123"><img src="/imgs/op.jpg"></a></div></div>
	    <div class="text"><h3><a href="https://mangakatana.com/manga/one-piece.123">One Piece <span class="hot">HOT</span></a></h3></div></div>
	  <div class="item"><div class="text"><h3><a href="/manga/one-punch-man.456">One-Punch Man</a></h3></div><img src="/imgs/opm.jpg"></div>
	</div><a class="next page-numbers" href="/page/2">Next</a>`

	mkatMangaHTML = `<html><head><link rel="canonical" href="https://mangakatana.com/manga/one-piece.123"></head><body>
	  <div class="media"><div class="cover"><img src="https://img.example/op.jpg"></div></div>
	  <h1 class="heading">One Piece</h1><div class="alt_name">ワンピース</div>
	  <ul><li><div class="value"><a class="author">Oda Eiichiro</a></div></li>
	      <li><div class="value status">Ongoing</div></li>
	      <li><div class="genres"><a href="/genre/action">Action</a><a href="/genre/adventure">Adventure</a></div></li></ul>
	  <div class="summary"><p>Gol D. Roger was known as the Pirate King.</p></div>
	  <table><tr><td><div class="chapter"><a href="https://mangakatana.com/manga/one-piece.123/c1100">Chapter 1100: Kuma</a></div></td>
	             <td><div class="update_time">Nov-24-2023</div></td></tr>
	         <tr><td><div class="chapter"><a href="/manga/one-piece.123/c1099.5">Chapter 1099.5</a></div></td>
	             <td><div class="update_time">yesterday</div></td></tr></table></body></html>`

	mkatReaderHTML = `<div id="imgs"></div><script>var thzq=['https://i1.example/1.jpg','https://i1.example/2.jpg',];
	  var ytaw=['https://decoy.example/x.jpg',];
	  $(function(){ for(i=0;i<thzq.length;i++){ $('#imgs').append($('<img>').attr('data-src', thzq[i])); } });</script>`
)

func newMangaKatana(t *testing.T) *mangakatana {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/page/1", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch q.Get("search") {
		case "one":
			if q.Get("search_by") != "book_name" {
				t.Errorf("search by %q", q.Get("search_by"))
			}
			_, _ = w.Write([]byte(mkatListHTML))
		case "one piece":
			// one match: the site answers with the manga's page itself
			_, _ = w.Write([]byte(mkatMangaHTML))
		default:
			t.Errorf("search %v", q)
		}
	})
	mux.HandleFunc("/manga/one-piece.123", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			t.Error("the site expects a referer")
		}
		_, _ = w.Write([]byte(mkatMangaHTML))
	})
	mux.HandleFunc("/manga/one-piece.123/c1100", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sv") != "mk" {
			t.Errorf("server %q", r.URL.Query().Get("sv"))
		}
		_, _ = w.Write([]byte(mkatReaderHTML))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &mangakatana{c: sourcekit.NewClient(srv.Client()), base: srv.URL}
}

// TestMangaKatana reads a manga the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestMangaKatana(t *testing.T) {
	k := newMangaKatana(t)
	ctx := context.Background()

	res, err := k.Search(ctx, "one", 1)
	if err != nil || len(res.Mangas) != 2 || !res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	// the title link also holds a badge, which is not part of the title
	if r := res.Mangas[0]; r.URL != "/manga/one-piece.123" || r.Title != "One Piece" || !strings.HasSuffix(r.CoverURL, "/imgs/op.jpg") {
		t.Fatalf("result %+v", r)
	}
	one, err := k.Search(ctx, "one piece", 1)
	if err != nil || len(one.Mangas) != 1 || one.Mangas[0].URL != "/manga/one-piece.123" || one.Mangas[0].Title != "One Piece" {
		t.Fatalf("single match: %v %+v", err, one)
	}

	d, err := k.Details(ctx, sourcekit.Ref{URL: "/manga/one-piece.123"})
	if err != nil || d.Title != "One Piece" || d.Author != "Oda Eiichiro" || d.Status != sourcekit.StatusOngoing ||
		strings.Join(d.Genres, ",") != "Action,Adventure" || d.CoverURL != "https://img.example/op.jpg" ||
		d.Description != "Gol D. Roger was known as the Pirate King.\n\nAlt name(s): ワンピース" {
		t.Fatalf("details: %v %+v", err, d)
	}

	chs, err := k.Chapters(ctx, sourcekit.Ref{URL: "/manga/one-piece.123"})
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	c := chs[0]
	if c.URL != "/manga/one-piece.123/c1100" || c.Name != "Chapter 1100: Kuma" || c.Number != 1100 ||
		c.UploadedAt == nil || c.UploadedAt.Format("2006-01-02") != "2023-11-24" {
		t.Fatalf("chapter %+v", c)
	}
	if chs[1].Number != 1099.5 || chs[1].UploadedAt != nil {
		t.Fatalf("chapter %+v", chs[1])
	}

	if err := k.SetOption("server", "mk"); err != nil {
		t.Fatal(err)
	}
	pages, err := k.Pages(ctx, sourcekit.PageRef{URL: c.URL})
	if err != nil || len(pages) != 2 || pages[1].URL != "https://i1.example/2.jpg" || pages[0].Headers["Referer"] == "" {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if err := k.SetOption("server", "9"); err == nil {
		t.Fatal("an unknown server was accepted")
	}
}
