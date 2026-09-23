package sites

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The answers below are shaped like what the Keiyoushi extension parses:
// the search box and browse listing answer JSON, a manga is a WordPress post
// with its terms embedded, and the manga's page carries its id and status.
const (
	mtaroSearchJSON = `{"results":[
	  {"id":1234,"slug":"solo-leveling","title":"Solo Leveling &amp;amp; More","thumbnail":"https://cdn.example/sl.webp",
	   "type":"Manhwa","description":"d","status":"Completed"},
	  {"id":99,"slug":"a-novel","title":"A Novel","thumbnail":"","type":"Novel","description":"","status":"Ongoing"}]}`

	mtaroBrowseJSON = `[{"id":"1234","url":"https://mangataro.org/manga/solo-leveling","title":"Solo Leveling",
	  "cover":"https://cdn.example/sl.webp","type":"Manhwa","description":"","status":"Completed"}]`

	mtaroPageHTML = `<html><body data-manga-id="1234"><span class="capitalize">manhwa</span>
	  <span class="capitalize">Completed</span></body></html>`

	mtaroPostJSON = `{"id":1234,"slug":"solo-leveling","title":{"rendered":"Solo Leveling"},
	  "content":{"rendered":"<p>E-rank hunter Sung Jinwoo&#8217;s story.</p>"},"type":"Manhwa",
	  "_embedded":{"wp:featuredmedia":[{"source_url":"https://cdn.example/sl-full.webp"}],
	   "wp:term":[[{"name":"Action","taxonomy":"post_tag"},{"name":"Fantasy","taxonomy":"post_tag"}],
	              [{"name":"Chugong","taxonomy":"manga_author"}]]}}`

	mtaroChaptersJSON = `{"chapters":[
	  {"url":"https://mangataro.org/read/solo-leveling/ch200-98765/","chapter":"200","title":"Epilogue",
	   "date":"2 days ago","group_name":"Team A","language":"en"},
	  {"url":"https://mangataro.org/read/solo-leveling/ch199.5-98764/","chapter":"199.5","title":"N/A",
	   "date":"Jan 5, 2024","group_name":null,"language":"EN"},
	  {"url":"https://mangataro.org/read/solo-leveling/ch200-es-5/","chapter":"200","date":"1 day ago","language":"es"}]}`

	mtaroPagesJSON = `{"images":["https://img.example/1.webp","https://img.example/2.webp"]}`
)

func newMangaTaro(t *testing.T, now time.Time) (*mangataro, *int) {
	t.Helper()
	pageHits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/search", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || string(body) != `{"limit":25,"query":"solo"}` {
			t.Errorf("search %s %s", r.Method, body)
		}
		if r.Header.Get("Content-Type") != "application/json" || r.Header.Get("Referer") == "" {
			t.Errorf("search headers %v", r.Header)
		}
		_, _ = w.Write([]byte(mtaroSearchJSON))
	})
	mux.HandleFunc("/wp-json/manga/v1/load", func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		// list filters go as strings holding JSON lists
		if p["sort"] != "popular_desc" || p["genres"] != "[]" || p["page"] != float64(1) || p["genreMatchMode"] != "any" {
			t.Errorf("load payload %v", p)
		}
		_, _ = w.Write([]byte(mtaroBrowseJSON))
	})
	mux.HandleFunc("/manga/solo-leveling", func(w http.ResponseWriter, r *http.Request) {
		pageHits++
		_, _ = w.Write([]byte(mtaroPageHTML))
	})
	mux.HandleFunc("/wp-json/wp/v2/manga/1234", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["_embed"]; !ok {
			t.Errorf("details without _embed: %s", r.URL)
		}
		_, _ = w.Write([]byte(mtaroPostJSON))
	})
	mux.HandleFunc("/auth/manga-chapters", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("manga_id") != "1234" || q.Get("limit") != "9999" || q.Get("order") != "DESC" ||
			q.Get("_t") != mangataroToken(now) || q.Get("_ts") == "" {
			t.Errorf("chapters query %v", q)
		}
		_, _ = w.Write([]byte(mtaroChaptersJSON))
	})
	mux.HandleFunc("/auth/chapter-content", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("chapter_id") != "98765" {
			t.Errorf("chapter id %q", r.URL.Query().Get("chapter_id"))
		}
		_, _ = w.Write([]byte(mtaroPagesJSON))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &mangataro{c: sourcekit.NewClient(srv.Client()), base: srv.URL, now: func() time.Time { return now }}, &pageHits
}

// TestMangaTaro reads a manga the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestMangaTaro(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	m, pageHits := newMangaTaro(t, now)
	ctx := context.Background()

	res, err := m.Search(ctx, "solo", 1)
	if err != nil || len(res.Mangas) != 1 || res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	// novels are dropped; titles come escaped, sometimes twice
	want := `{"id":"1234","slug":"solo-leveling"}`
	if res.Mangas[0].URL != want || res.Mangas[0].Title != "Solo Leveling & More" || res.Mangas[0].ID != "1234" {
		t.Fatalf("result %+v", res.Mangas[0])
	}
	pop, err := m.Popular(ctx, 1)
	if err != nil || len(pop.Mangas) != 1 || pop.Mangas[0].URL != want || pop.HasNext {
		t.Fatalf("popular: %v %+v", err, pop)
	}

	d, err := m.Details(ctx, sourcekit.Ref{URL: want})
	if err != nil || d.Title != "Solo Leveling" || d.Author != "Chugong" || d.Status != sourcekit.StatusCompleted ||
		d.Description != "E-rank hunter Sung Jinwoo’s story." || d.CoverURL != "https://cdn.example/sl-full.webp" ||
		strings.Join(d.Genres, ",") != "Action,Fantasy,Manhwa" || d.URL != want || !strings.HasSuffix(d.WebURL, "/manga/solo-leveling") {
		t.Fatalf("details: %v %+v", err, d)
	}

	// the id is in the stored url: the chapter list needs no page visit
	hits := *pageHits
	chs, err := m.Chapters(ctx, sourcekit.Ref{URL: want})
	if err != nil || len(chs) != 2 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	if *pageHits != hits {
		t.Fatal("the chapter list fetched the manga's page")
	}
	c := chs[0]
	if c.URL != "/read/solo-leveling/ch200-98765" || c.Name != "Chapter 200: Epilogue" || c.Number != 200 ||
		c.Scanlator != "Team A" || c.UploadedAt == nil || !c.UploadedAt.Equal(now.AddDate(0, 0, -2)) {
		t.Fatalf("chapter %+v", c)
	}
	if chs[1].Name != "Chapter 199.5" || chs[1].Number != 199.5 || chs[1].Scanlator != "" || chs[1].UploadedAt != nil {
		t.Fatalf("chapter %+v", chs[1])
	}

	pages, err := m.Pages(ctx, sourcekit.PageRef{URL: c.URL})
	if err != nil || len(pages) != 2 || pages[1].URL != "https://img.example/2.webp" || pages[0].Headers["Referer"] == "" {
		t.Fatalf("pages: %v %+v", err, pages)
	}
}

// TestMangaTaroLink: a plain "/manga/<slug>" link works too; the manga's page
// supplies the id.
func TestMangaTaroLink(t *testing.T) {
	m, _ := newMangaTaro(t, time.Now())
	d, err := m.Details(context.Background(), sourcekit.Ref{URL: "https://mangataro.org/manga/solo-leveling"})
	if err != nil || d.ID != "1234" || d.URL != `{"id":"1234","slug":"solo-leveling"}` {
		t.Fatalf("details: %v %+v", err, d)
	}
}

// TestMangaTaroToken: the chapter list's token is md5 of the time and the
// hour, cut to 16 hex digits.
func TestMangaTaroToken(t *testing.T) {
	now := time.Date(2026, 9, 23, 7, 30, 0, 0, time.FixedZone("x", 3*3600))
	// md5("1790137800mng_ch_2026092304")
	if got := mangataroToken(now); got != "299be7f78e1780a6" {
		t.Fatalf("token %q", got)
	}
}

// TestMangaTaroAgo reads the site's relative dates.
func TestMangaTaroAgo(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"5 minutes ago": now.Add(-5 * time.Minute), "1 hour ago": now.Add(-time.Hour),
		"3 weeks ago": now.AddDate(0, 0, -21), "1 year ago": now.AddDate(-1, 0, 0),
	} {
		if got := mangataroAgo(in, now); got == nil || !got.Equal(want) {
			t.Fatalf("mangataroAgo(%q) = %v", in, got)
		}
	}
	if mangataroAgo("yesterday", now) != nil {
		t.Fatal("an unknown date should stay unset")
	}
}
