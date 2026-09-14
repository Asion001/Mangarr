package kavita

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

func TestKavitaRescanAndProgress(t *testing.T) {
	var scanned []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/Plugin/authenticate":
			key := r.URL.Query().Get("apiKey")
			_, _ = io.WriteString(w, `{"username":"user-`+key+`","token":"tok-`+key+`"}`)
		case r.URL.Path == "/api/Library/scan-folder":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["apiKey"] != "admin" {
				t.Errorf("scan-folder must use the admin key: %v", body)
			}
			scanned = append(scanned, body["folderPath"].(string))
		case r.URL.Path == "/api/Series/all-v2":
			if r.Header.Get("Authorization") != "Bearer tok-reader" {
				t.Errorf("expected reader token, got %q", r.Header.Get("Authorization"))
			}
			_, _ = io.WriteString(w, `[{"id":5,"pagesRead":30,"folderPath":"/manga/One Piece"},{"id":6,"pagesRead":0,"folderPath":"/manga/Other"}]`)
		case r.URL.Path == "/api/Series/volumes":
			if r.URL.Query().Get("seriesId") != "5" {
				t.Errorf("unexpected series %s", r.URL.RawQuery)
			}
			_, _ = io.WriteString(w, `[{"chapters":[
				{"pages":20,"pagesRead":20,"lastReadingProgressUtc":"2026-01-02T03:04:05Z","files":[{"filePath":"/manga/One Piece/One Piece Ch.0001.cbz"}]},
				{"pages":20,"pagesRead":10,"lastReadingProgressUtc":"0001-01-01T00:00:00","files":[{"filePath":"/manga/One Piece/One Piece Ch.0002.cbz"}]},
				{"pages":20,"pagesRead":0,"files":[{"filePath":"/manga/One Piece/One Piece Ch.0003.cbz"}]}]}]`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	impl, _ := modules.Lookup(modules.KindLibrary, "kavita")
	s, err := modules.DecodeSettings(impl, map[string]any{"url": srv.URL, "apiKey": "admin", "pathMappings": map[string]any{"/data/manga": "/manga"}})
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := impl.New(modules.Deps{HTTP: srv.Client()}, s)
	m := inst.(*Module)
	ctx := context.Background()
	if err := m.Test(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Rescan(ctx, []string{"/data/manga/One Piece"}); err != nil || len(scanned) != 1 || scanned[0] != "/manga/One Piece" {
		t.Fatalf("rescan: %v %v", err, scanned)
	}
	acc := library.Account{Credentials: map[string]string{"apiKey": "reader"}}
	if u, err := m.TestAccount(ctx, acc); err != nil || u != "user-reader" {
		t.Fatalf("account: %v %v", u, err)
	}
	prog, err := m.ReadProgress(ctx, acc, []string{"/data/manga"})
	if err != nil {
		t.Fatal(err)
	}
	if len(prog) != 2 || !strings.HasSuffix(prog[0].LocalPath, "/data/manga/One Piece/One Piece Ch.0001.cbz") || !prog[0].Completed || prog[0].ReadAt == nil ||
		prog[1].Completed || prog[1].Page != 10 || prog[1].ReadAt != nil {
		b, _ := json.Marshal(prog)
		t.Fatalf("progress: %s", b)
	}
}
