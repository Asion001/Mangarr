package sites

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The answers below are shaped like what the Keiyoushi extension parses:
// everything is wrapped in {"data": ...}, listings in {"list": [...]}, and
// images are an id and a format ("f") on the CDN.
const (
	mcloudLibraryJSON = `{"data":[{"id":"c1","title":"Omniscient Reader","cover":{"id":"cov1","f":"webp"}}]}`

	mcloudPopularJSON = `{"data":{"list":[{"id":"c1","title":"Omniscient Reader","cover":{"id":"cov1","f":"webp"}},
	  {"id":"c2","title":"Other","cover":{"id":"cov2","f":"jpg"}}]}}`

	mcloudComicJSON = `{"data":{"id":"c1","title":"Omniscient Reader","alt_titles":"ORV • Jeonjijeok",
	  "nat_titles":"전지적 독자 시점","description":" Only I know the end. ","status":"Ongoing","start_year":2020,
	  "type":"Manhwa","authors":"sing N song • UMI","artists":"Sleepy-C","links":{"al":119257},
	  "tags":[{"id":"t2","name":"Isekai","type":"theme"},{"id":"t1","name":"Action","type":"genre"}],
	  "cover":{"id":"cov1","f":"webp"},
	  "chapters":[{"id":"ch2","number":201.5,"name":"Side story","created_date":"2026-09-20T10:11:12.000Z"},
	              {"id":"ch1","number":201,"created_date":"2026-09-13T10:11:12"}]}}`

	mcloudChapterJSON = `{"data":{"id":"ch2","comic_id":"c1","images":[{"id":"p1","f":"webp"},{"id":"p2","f":"png"}]}}`
)

func newMangaCloud(t *testing.T) *mangacloud {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/comic/library", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || string(body) != `{"title":"omniscient","page":1}` {
			t.Errorf("library %s %s", r.Method, body)
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Referer") == "" {
			t.Errorf("library headers %v", r.Header)
		}
		_, _ = w.Write([]byte(mcloudLibraryJSON))
	})
	mux.HandleFunc("/comic-popular-view/week", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(mcloudPopularJSON)) })
	mux.HandleFunc("/comic/c1", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(mcloudComicJSON)) })
	mux.HandleFunc("/chapters/ch2", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(mcloudChapterJSON)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &mangacloud{c: sourcekit.NewClient(srv.Client()), site: "https://mangacloud.example", api: srv.URL, cdn: "https://cdn.example"}
}

// TestMangaCloud reads a manga the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestMangaCloud(t *testing.T) {
	m := newMangaCloud(t)
	ctx := context.Background()

	if _, err := m.Search(ctx, "om", 1); err == nil {
		t.Fatal("the site refuses searches under three characters")
	}
	res, err := m.Search(ctx, "omniscient", 1)
	if err != nil || len(res.Mangas) != 1 || res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	if r := res.Mangas[0]; r.URL != "c1" || r.Title != "Omniscient Reader" || r.CoverURL != "https://cdn.example/c1/cov1.webp" {
		t.Fatalf("result %+v", r)
	}
	pop, err := m.Popular(ctx, 2)
	if err != nil || len(pop.Mangas) != 2 || !pop.HasNext {
		t.Fatalf("popular: %v %+v", err, pop)
	}

	d, err := m.Details(ctx, sourcekit.Ref{URL: "c1"})
	if err != nil || d.Title != "Omniscient Reader" || d.Author != "sing N song, UMI" || d.Artist != "Sleepy-C" ||
		d.Status != sourcekit.StatusOngoing || strings.Join(d.Genres, ",") != "Manhwa,Action,Isekai" ||
		d.WebURL != "https://mangacloud.example/comic/c1" {
		t.Fatalf("details: %v %+v", err, d)
	}
	if !strings.HasPrefix(d.Description, "Only I know the end.\n\nYear: 2020\n\nAlternative Name(s):\n- ORV\n- Jeonjijeok\n- 전지적") {
		t.Fatalf("description %q", d.Description)
	}

	// a web link finds the same manga
	chs, err := m.Chapters(ctx, sourcekit.Ref{URL: "https://mangacloud.org/comic/c1"})
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	c := chs[0]
	if c.URL != `{"comicId":"c1","chapterId":"ch2"}` || c.Name != "Chapter 201.5 - Side story" || c.Number != 201.5 ||
		c.UploadedAt == nil || c.UploadedAt.Day() != 20 || c.WebURL != "https://mangacloud.example/comic/c1/chapter/ch2" {
		t.Fatalf("chapter %+v", c)
	}
	if chs[1].Name != "Chapter 201" || chs[1].UploadedAt == nil {
		t.Fatalf("chapter %+v", chs[1])
	}

	pages, err := m.Pages(ctx, sourcekit.PageRef{URL: c.URL})
	if err != nil || len(pages) != 2 || pages[1].URL != "https://cdn.example/c1/ch2/p2.png" {
		t.Fatalf("pages: %v %+v", err, pages)
	}
}
