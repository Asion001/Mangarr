package sites

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Asion001/mangarr/internal/sources/sourcekit"
)

// The answers below have the shapes the extension reads: everything comes
// wrapped in "data", with a "meta" carrying has_more where the endpoint pages.
const (
	sggSearchJSON = `{"data":[{"id":42,"title":"Solo Leveling","cover":"solo.webp"},{"id":7,"title":"Other"}]}`

	sggSeriesJSON = `{"data":{"id":42,"title":"Solo Leveling","summary":"A weak hunter.","cover":"solo.webp",
	  "author":["Chugong"],"artist":["DUBU","Redice"],"tags":[10,1,999],"status":2}}`

	sggLatestJSON = `{"data":[{"id":42,"title":"Solo Leveling","cover":"solo.webp"}],"meta":{"has_more":true}}`

	sggChapters1JSON = `{"data":[
	  {"id":1002,"number":201,"title":"Epilogue","created_at":"2024-05-01 08:30:00","group_id":3,"group":{"title":"Team A"}},
	  {"id":1001,"number":200.5,"created_at":"2024-04-20 00:00:00"}],"meta":{"has_more":true}}`

	sggChapters2JSON = `{"data":[{"id":1000,"number":200,"created_at":"bad date"}],"meta":{"has_more":false}}`

	sggPagesJSON = `{"data":{"chapter":{"id":1002,"pages":[{"position":2,"path":"b.webp"},{"position":1,"path":"a.webp"}]}}}`
)

func newScansGG(t *testing.T) *scansgg {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/series", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("id") != "" {
			if q.Get("id") != "42" || q.Get("trackers") != "true" || q.Get("sources") != "true" {
				t.Errorf("series query %v", q)
			}
			_, _ = w.Write([]byte(sggSeriesJSON))
			return
		}
		if q.Get("limit") != "21" || q.Get("offset") != "21" || q.Get("q") != "solo" || q.Get("q_tags") != "[]" {
			t.Errorf("search query %v", q)
		}
		_, _ = w.Write([]byte(sggSearchJSON))
	})
	mux.HandleFunc("/chapters", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("series_details") == "true":
			if q.Get("sort") != "date" || q.Get("limit") != "14" {
				t.Errorf("latest query %v", q)
			}
			_, _ = w.Write([]byte(sggLatestJSON))
		case q.Get("series_id") != "42" || q.Get("group_details") != "true" || q.Get("limit") != "100":
			t.Errorf("chapters query %v", q)
		case q.Get("page") == "1":
			_, _ = w.Write([]byte(sggChapters1JSON))
		default:
			_, _ = w.Write([]byte(sggChapters2JSON))
		}
	})
	mux.HandleFunc("/chapter-navigation", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("series_id") != "42" || q.Get("chapter_id") != "1002" || q.Get("group_id") != "3" {
			t.Errorf("pages query %v", q)
		}
		_, _ = w.Write([]byte(sggPagesJSON))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &scansgg{c: sourcekit.NewClient(srv.Client()), site: "https://scans.example", api: srv.URL, cdn: "https://cdn.example/uploads"}
}

// TestScansGG reads a series the way the library does: find it, read its
// details, list chapters, then a chapter's pages.
func TestScansGG(t *testing.T) {
	s := newScansGG(t)
	ctx := context.Background()

	res, err := s.Search(ctx, "solo", 2)
	if err != nil || len(res.Mangas) != 2 || res.HasNext {
		t.Fatalf("search: %v %+v", err, res)
	}
	got := res.Mangas[0]
	if got.URL != "42" || got.ID != "42" || got.Title != "Solo Leveling" || got.CoverURL != "https://cdn.example/uploads/covers/solo.webp" ||
		res.Mangas[1].CoverURL != "" {
		t.Fatalf("results %+v", res.Mangas)
	}
	if latest, err := s.Latest(ctx, 1); err != nil || len(latest.Mangas) != 1 || !latest.HasNext {
		t.Fatalf("latest: %v %+v", err, latest)
	}

	d, err := s.Details(ctx, sourcekit.Ref{URL: got.URL})
	if err != nil || d.Title != "Solo Leveling" || d.Author != "Chugong" || d.Artist != "DUBU, Redice" ||
		d.Status != sourcekit.StatusCompleted || strings.Join(d.Genres, "|") != "Action|Fantasy" ||
		d.WebURL != "https://scans.example/series/42" {
		t.Fatalf("details: %v %+v", err, d)
	}

	chs, err := s.Chapters(ctx, sourcekit.Ref{URL: got.URL})
	if err != nil || len(chs) != 3 {
		t.Fatalf("chapters: %v %+v", err, chs)
	}
	c := chs[0]
	if c.Name != "Chapter 201 - Epilogue" || c.Number != 201 || c.Scanlator != "Team A" || c.UploadedAt == nil ||
		c.UploadedAt.Hour() != 8 || c.URL != "/chapter-navigation?series_id=42&chapter_id=1002&group_id=3" {
		t.Fatalf("chapter %+v", c)
	}
	if chs[1].Name != "Chapter 200.5" || !strings.HasSuffix(chs[1].URL, "group_id=0") || chs[2].UploadedAt != nil {
		t.Fatalf("chapters %+v", chs[1:])
	}

	pages, err := s.Pages(ctx, sourcekit.PageRef{URL: c.URL})
	if err != nil || len(pages) != 2 {
		t.Fatalf("pages: %v %+v", err, pages)
	}
	if pages[0].URL != "https://cdn.example/uploads/pages/1002/a.webp" || pages[1].URL != "https://cdn.example/uploads/pages/1002/b.webp" {
		t.Fatalf("pages are in position order: %+v", pages)
	}
}
