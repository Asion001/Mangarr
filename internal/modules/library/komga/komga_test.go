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
