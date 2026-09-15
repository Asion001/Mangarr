package komga

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/library"
)

func newTestModule(t *testing.T, url string) *Module {
	t.Helper()
	impl, _ := modules.Lookup(modules.KindLibrary, "komga")
	st, err := modules.DecodeSettings(impl, map[string]any{"url": url, "apiKey": "admin", "pathMappings": map[string]any{"/data": "/books"}})
	if err != nil {
		t.Fatal(err)
	}
	inst, err := impl.New(modules.Deps{HTTP: &http.Client{Timeout: 10 * time.Second}}, st)
	if err != nil {
		t.Fatal(err)
	}
	return inst.(*Module)
}

func TestWatchProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		switch r.URL.Path {
		case "/sse/v1/events":
			if key != "reader" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			f := w.(http.Flusher)
			for _, chunk := range []string{
				":heartbeat\n\n",
				"event:TaskQueueStatus\ndata:{\"count\":1}\n\n",
				"event:ReadProgressChanged\ndata:{\"bookId\":\"B1\",\"userId\":\"u\"}\n\n",
				"event:ReadProgressDeleted\ndata:{\"bookId\":\"B2\",\"userId\":\"u\"}\n\n",
				"event:ReadProgressSeriesChanged\ndata:{\"seriesId\":\"S1\",\"userId\":\"u\"}\n\n",
			} {
				_, _ = io.WriteString(w, chunk)
				f.Flush()
			}
		case "/api/v1/books/B1":
			_, _ = io.WriteString(w, `{"id":"B1","seriesId":"S1","url":"Ch.0001.cbz","readProgress":{"page":20,"completed":true,"readDate":"2026-01-02T03:04:05Z"}}`)
		case "/api/v1/books/B2":
			_, _ = io.WriteString(w, `{"id":"B2","seriesId":"S1","url":"Ch.0002.cbz"}`)
		case "/api/v1/series":
			_, _ = io.WriteString(w, `{"content":[{"id":"S1","url":"/books/manga/One Piece"},{"id":"S2","url":"/books/manga/Other"}]}`)
		case "/api/v1/series/S1":
			if key != "admin" {
				t.Errorf("series lookups use the admin key")
			}
			_, _ = io.WriteString(w, `{"id":"S1","url":"/books/manga/One Piece"}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	m := newTestModule(t, srv.URL)

	var mu sync.Mutex
	var evs []library.ProgressEvent
	err := m.WatchProgress(context.Background(), library.Account{Credentials: map[string]string{"apiKey": "reader"}}, func(ev library.ProgressEvent) {
		mu.Lock()
		evs = append(evs, ev)
		mu.Unlock()
	})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("err = %v", err)
	}
	if len(evs) != 4 || !evs[0].Resync || !evs[3].Resync {
		t.Fatalf("events %+v", evs)
	}
	b1, b2 := evs[1], evs[2]
	if b1.Book == nil || b1.Book.LocalPath != "/data/manga/One Piece/Ch.0001.cbz" || !b1.Book.Completed || b1.Deleted || b1.Book.ReadAt == nil {
		t.Fatalf("book 1: %+v %+v", b1, b1.Book)
	}
	if b2.Book == nil || !b2.Deleted || b2.Book.LocalPath != "/data/manga/One Piece/Ch.0002.cbz" {
		t.Fatalf("book 2: %+v", b2)
	}
	if u, err := m.SeriesURL(context.Background(), "/data/manga/Other"); err != nil || u != srv.URL+"/series/S2" {
		t.Fatalf("series url %q %v", u, err)
	}
	if u, _ := m.SeriesURL(context.Background(), "/data/manga/Missing"); u != "" {
		t.Fatalf("unknown folder linked to %q", u)
	}
}

func TestWatchProgressIdle(t *testing.T) {
	old := IdleTimeout
	IdleTimeout = 200 * time.Millisecond
	defer func() { IdleTimeout = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done() // never sends anything
	}))
	defer srv.Close()
	err := newTestModule(t, srv.URL).WatchProgress(context.Background(), library.Account{Credentials: map[string]string{"apiKey": "reader"}}, func(library.ProgressEvent) {})
	if err == nil || !strings.Contains(err.Error(), "quiet") {
		t.Fatalf("err = %v", err)
	}
}
