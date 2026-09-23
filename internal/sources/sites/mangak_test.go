package sites

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The answers below have the shapes the extension reads: the API wraps
// everything in "data", the manga page hydrates from a Pages Router
// __NEXT_DATA__ script, and the chapter page from App Router Flight rows
// that share values by reference.
const (
	mkSearchJSON = `{"data":{"items":[
	  {"id":"abc123","name":"Solo Leveling","cover":"https://cdn.example/abc123.jpg","url":"/solo-leveling"},
	  {"id":"def456","name":"Other","cover":"/covers/def456.jpg","url":"/other"}],
	  "pagination":{"has_next":true}}}`

	mkMangaHTML = `<html><body><div id="__next"></div>
<script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"initialManga":{
  "id":"abc123","name":"Solo Leveling","summary":"A weak hunter.","status":"Completed",
  "cover":"https://cdn.example/abc123.jpg","url":"/solo-leveling",
  "authors":[{"name":"Chugong"},{"name":"DUBU"}],"genres":[{"name":"Action"},{"name":"Fantasy"}]}}},
  "page":"/[slug]","query":{"slug":"solo-leveling"}}</script></body></html>`

	mkChaptersJSON = `{"data":{"chapters":[
	  {"url":"/solo-leveling/chapter-1","name":"Chapter 1","updated_at":"2024-01-01T10:00:00Z","chapter_number":1},
	  {"url":"/solo-leveling/chapter-2-5","name":"Chapter 2.5","updated_at":"2024-01-03T10:00:00.000Z","chapter_number":2.5},
	  {"url":"/solo-leveling/chapter-2","name":"Chapter 2","updated_at":"2024-01-02T10:00:00Z","chapter_number":2}]}}`
)

// mkFlightPage is a chapter page whose data comes in two pushes: the root
// row points at row 2 for its props, and row 2 at a text chunk (row 3) for
// its first image.
func mkFlightPage(t *testing.T) string {
	t.Helper()
	img := "https://cdn.example/ch2/1.jpg"
	chunks := []string{
		"1:I[\"chunks/page.js\",[],\"Page\"]\n0:[\"$\",\"$L1\",null,{\"pageProps\":\"$2\"}]\n",
		"2:{\"initialChapter\":{\"images\":[\"$3\",\"https://cdn.example/ch2/2.jpg\"]},\"note\":\"$$5 off\"}\n" +
			fmt.Sprintf("3:T%x,%s", len(img), img),
	}
	var b strings.Builder
	b.WriteString("<html><body><script src=\"/app.js\"></script>")
	for _, c := range chunks {
		push, err := json.Marshal([]any{1, c})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, "<script>self.__next_f.push(%s)</script>", push)
	}
	b.WriteString("</body></html>")
	return b.String()
}

func newMangaK(t *testing.T) *mkSite {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/titles/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("limit") != "24" || q.Get("page") != "1" {
			t.Errorf("search paging %v", q)
		}
		if q.Get("q") != "" && q.Get("q") != "Solo Leveling" {
			t.Errorf("the search should be letters, digits and spaces: %q", q.Get("q"))
		}
		if q.Get("sort") == "popular" && q.Get("window") != "week" {
			t.Errorf("popular window %q", q.Get("window"))
		}
		_, _ = w.Write([]byte(mkSearchJSON))
	})
	mux.HandleFunc("/titles/abc123/chapters", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cv") == "" {
			t.Error("the chapter list is asked for with a cache buster")
		}
		_, _ = w.Write([]byte(mkChaptersJSON))
	})
	mux.HandleFunc("/solo-leveling", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(mkMangaHTML)) })
	mux.HandleFunc("/solo-leveling/chapter-2", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") == "" {
			t.Error("the site expects a referer")
		}
		_, _ = w.Write([]byte(mkFlightPage(t)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	// one server plays both the site and its API
	return &mkSite{c: sourcekit.NewClient(srv.Client()), id: "test", name: "MangaK", base: srv.URL, api: srv.URL}
}

// TestMangaK reads a series the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestMangaK(t *testing.T) {
	m := newMangaK(t)
	ctx := context.Background()

	res, err := m.Search(ctx, "Solo: Leveling!", 1)
	if err != nil || len(res.Mangas) != 2 || !res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	got := res.Mangas[0]
	if got.Title != "Solo Leveling" || got.URL != "/solo-leveling" || got.ID != "abc123" || got.CoverURL != "https://cdn.example/abc123.jpg" {
		t.Fatalf("result %+v", got)
	}
	if res.Mangas[1].CoverURL != m.base+"/covers/def456.jpg" {
		t.Fatalf("relative cover %+v", res.Mangas[1])
	}
	if pop, err := m.Popular(ctx, 1); err != nil || len(pop.Mangas) != 2 {
		t.Fatalf("popular: %v %+v", err, pop)
	}

	d, err := m.Details(ctx, sourcekit.Ref{URL: got.URL, ID: got.ID})
	if err != nil || d.Title != "Solo Leveling" || d.ID != "abc123" || d.Author != "Chugong, DUBU" ||
		d.Status != sourcekit.StatusCompleted || len(d.Genres) != 2 || d.Description != "A weak hunter." {
		t.Fatalf("details: %v %+v", err, d)
	}

	// no stored id (a backup import): the manga page supplies it
	chs, err := m.Chapters(ctx, sourcekit.Ref{URL: got.URL})
	if err != nil || len(chs) != 3 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if chs[0].Number != 2.5 || chs[1].Number != 2 || chs[2].Number != 1 || chs[1].URL != "/solo-leveling/chapter-2" ||
		chs[0].UploadedAt == nil || chs[0].UploadedAt.Day() != 3 {
		t.Fatalf("chapters are newest first: %+v", chs)
	}

	pages, err := m.Pages(ctx, sourcekit.PageRef{URL: chs[1].URL})
	if err != nil || len(pages) != 2 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if pages[0].URL != "https://cdn.example/ch2/1.jpg" || pages[1].URL != "https://cdn.example/ch2/2.jpg" ||
		pages[0].Headers["Referer"] == "" {
		t.Fatalf("pages %+v", pages)
	}
}

// TestMangaKNextData: references between Flight rows are followed, paths
// into a row included, and the escapes Flight uses are undone.
func TestMangaKNextData(t *testing.T) {
	flight := "5:{\"title\":\"Deep\",\"list\":[\"a\",{\"x\":\"$$literal\"}]}\n" +
		"0:{\"wanted\":{\"title\":\"$5:title\",\"second\":\"$5:list:1\",\"gone\":\"$undefined\",\"loop\":\"$0\"}}\n"
	push, _ := json.Marshal([]any{1, flight})
	doc, err := goquery.NewDocumentFromReader(strings.NewReader("<script>self.__next_f.push(" + string(push) + ");</script>"))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Wanted struct {
			Title  string            `json:"title"`
			Second map[string]string `json:"second"`
			Gone   *string           `json:"gone"`
		} `json:"wanted"`
	}
	found, err := mkNextData(doc, "wanted", &out)
	if err != nil || !found {
		t.Fatalf("mkNextData: %v %v", found, err)
	}
	if out.Wanted.Title != "Deep" || out.Wanted.Second["x"] != "$literal" || out.Wanted.Gone != nil {
		t.Fatalf("resolved %+v", out)
	}
	if found, _ := mkNextData(doc, "missing", &out); found {
		t.Fatal("a key that isn't there was found")
	}
}

// TestMangaKQuery: the API only takes letters, digits and spaces, at most 50.
func TestMangaKQuery(t *testing.T) {
	if got := mkQuery("  Re:Zero — Starting Life!  "); got != "ReZero  Starting Life" {
		t.Fatalf("mkQuery = %q", got)
	}
	if got := mkQuery(strings.Repeat("ab", 40)); len(got) != 50 {
		t.Fatalf("a long search is cut to 50: %d", len(got))
	}
	if mkAPIFor("https://mangak.io") != "https://api.mangak.io" {
		t.Fatal(mkAPIFor("https://mangak.io"))
	}
}
