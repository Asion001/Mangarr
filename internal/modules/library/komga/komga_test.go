package komga

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/library"
)

func TestKomgaRescanAndProgress(t *testing.T) {
	var scanned []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v2/users/me":
			if key == "admin" {
				_, _ = io.WriteString(w, `{"id":"1","email":"admin@x","roles":["ADMIN"]}`)
			} else {
				_, _ = io.WriteString(w, `{"id":"2","email":"reader@x","roles":["USER"]}`)
			}
		case r.URL.Path == "/api/v1/libraries":
			_, _ = io.WriteString(w, `[{"id":"L1","name":"Manga","root":"/books/manga"},{"id":"L2","name":"Other","root":"/books/other"}]`)
		case strings.HasSuffix(r.URL.Path, "/scan"):
			scanned = append(scanned, r.URL.Path)
		case r.URL.Path == "/api/v1/series/list":
			if key != "admin" {
				t.Errorf("series list must use the admin key")
			}
			_, _ = io.WriteString(w, `{"content":[{"id":"S1","url":"/books/manga/One Piece"}]}`)
		case r.URL.Path == "/api/v1/books/list":
			if key != "reader" || !strings.Contains(string(body), `"readStatus"`) {
				t.Errorf("books list: key=%s body=%s", key, body)
			}
			// non-admin users only get the file name
			_, _ = io.WriteString(w, `{"content":[
				{"seriesId":"S1","url":"One Piece Ch.0001.cbz","readProgress":{"page":20,"completed":true,"readDate":"2026-01-02T03:04:05Z"}},
				{"seriesId":"S1","url":"One Piece Ch.0002.cbz","readProgress":{"page":3,"completed":false}},
				{"seriesId":"S9","url":"x.cbz","readProgress":{"page":1,"completed":true}}]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	impl, _ := modules.Lookup(modules.KindLibrary, "komga")
	s, err := modules.DecodeSettings(impl, map[string]any{"url": srv.URL, "apiKey": "admin", "pathMappings": map[string]any{"/data/manga": "/books/manga"}})
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := impl.New(modules.Deps{HTTP: srv.Client()}, s)
	m := inst.(*Module)
	ctx := context.Background()
	if err := m.Test(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Rescan(ctx, []string{"/data/manga/One Piece"}); err != nil || len(scanned) != 1 || scanned[0] != "/api/v1/libraries/L1/scan" {
		t.Fatalf("rescan: %v %v", err, scanned)
	}
	user, err := m.TestAccount(ctx, library.Account{Credentials: map[string]string{"apiKey": "reader"}})
	if err != nil || user != "reader@x" {
		t.Fatalf("account: %v %v", user, err)
	}
	prog, err := m.ReadProgress(ctx, library.Account{Credentials: map[string]string{"apiKey": "reader"}}, []string{"/data/manga"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(prog)
	if len(prog) != 2 || prog[0].LocalPath != "/data/manga/One Piece/One Piece Ch.0001.cbz" || !prog[0].Completed || prog[0].ReadAt == nil || prog[1].Completed {
		t.Fatalf("progress: %s", b)
	}
}

func TestKomgaVerifyBook(t *testing.T) {
	status, width := "READY", 1200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/libraries":
			_, _ = io.WriteString(w, `[{"id":"L1","name":"Manga","root":"/books/manga"}]`)
		case "/api/v1/series/list":
			_, _ = io.WriteString(w, `{"content":[{"id":"S1","url":"/books/manga/Other"},{"id":"S2","url":"/books/manga/One Piece"}]}`)
		case "/api/v1/series/S2/books":
			_, _ = io.WriteString(w, `{"content":[{"id":"B1","url":"/books/manga/One Piece/One Piece Ch.0001.cbz","media":{"status":"`+status+`","pagesCount":20,"comment":"ERR_1234"}}]}`)
		case "/api/v1/books/B1/pages":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"number": 1, "mediaType": "image/avif", "width": width, "height": 1800}})
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	impl, _ := modules.Lookup(modules.KindLibrary, "komga")
	s, _ := modules.DecodeSettings(impl, map[string]any{"url": srv.URL, "apiKey": "admin", "pathMappings": map[string]any{"/data/manga": "/books/manga"}})
	inst, _ := impl.New(modules.Deps{HTTP: srv.Client()}, s)
	v := inst.(library.Verifier)
	ctx := context.Background()
	bc, err := v.VerifyBook(ctx, "/data/manga/One Piece/One Piece Ch.0001.cbz")
	if err != nil || !bc.Found || bc.Problem != "" || bc.PageWidth != 1200 || bc.Pages != 20 {
		t.Fatalf("ready book: %+v %v", bc, err)
	}
	width = 0
	if bc, _ = v.VerifyBook(ctx, "/data/manga/One Piece/One Piece Ch.0001.cbz"); bc.Problem == "" {
		t.Fatal("pages without dimensions must be reported")
	}
	status = "ERROR"
	if bc, _ = v.VerifyBook(ctx, "/data/manga/One Piece/One Piece Ch.0001.cbz"); !strings.Contains(bc.Problem, "ERR_1234") {
		t.Fatalf("error status: %+v", bc)
	}
	if bc, _ = v.VerifyBook(ctx, "/data/manga/One Piece/missing.cbz"); bc.Found {
		t.Fatal("unknown file must not be found")
	}
}

func TestKomgaWriteProgress(t *testing.T) {
	var patched []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/api/v1/libraries":
			_, _ = io.WriteString(w, `[{"id":"L1","name":"Manga","root":"/books/manga"}]`)
		case r.URL.Path == "/api/v1/series/list":
			_, _ = io.WriteString(w, `{"content":[{"id":"S2","url":"/books/manga/One Piece"}]}`)
		case r.URL.Path == "/api/v1/series/S2/books":
			_, _ = io.WriteString(w, `{"content":[{"id":"B1","url":"/books/manga/One Piece/One Piece Ch.0001.cbz"},{"id":"B2","url":"/books/manga/One Piece/One Piece Ch.0002.cbz"}]}`)
		case r.Method == http.MethodPatch && strings.HasSuffix(r.URL.Path, "/read-progress"):
			if r.Header.Get("X-API-Key") != "reader" {
				t.Errorf("progress must be written with the reader's key")
			}
			patched = append(patched, r.URL.Path+" "+string(body))
		case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/read-progress"):
			if r.Header.Get("X-API-Key") != "reader" {
				t.Errorf("progress must be cleared with the reader's key")
			}
			patched = append(patched, "DELETE "+r.URL.Path)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	impl, _ := modules.Lookup(modules.KindLibrary, "komga")
	s, _ := modules.DecodeSettings(impl, map[string]any{"url": srv.URL, "apiKey": "admin", "pathMappings": map[string]any{"/data/manga": "/books/manga"}})
	inst, _ := impl.New(modules.Deps{HTTP: srv.Client()}, s)
	pw := inst.(library.ProgressWriter)
	n, missing, err := pw.WriteProgress(context.Background(), library.Account{Credentials: map[string]string{"apiKey": "reader"}}, []library.BookProgress{
		{LocalPath: "/data/manga/One Piece/One Piece Ch.0001.cbz", Completed: true},
		{LocalPath: "/data/manga/One Piece/One Piece Ch.0002.cbz", Page: 7},
		{LocalPath: "/data/manga/One Piece/One Piece Ch.0003.cbz", Completed: true},
		{LocalPath: "/data/manga/One Piece/One Piece Ch.0001.cbz", Unread: true},
	})
	if err != nil || n != 3 || len(missing) != 1 {
		t.Fatalf("write: %d %v %v", n, missing, err)
	}
	if len(patched) != 3 || !strings.Contains(patched[0], `"completed":true`) || !strings.Contains(patched[1], `"page":7`) ||
		patched[2] != "DELETE /api/v1/books/B1/read-progress" {
		t.Fatalf("patches: %v", patched)
	}
}
