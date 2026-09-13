package suwayomi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/source"
)

// TestOperationsMatchSchema validates every GraphQL operation against the
// pinned Suwayomi schema. Bumping the Suwayomi version = replace the schema
// file and fix what breaks here.
func TestOperationsMatchSchema(t *testing.T) {
	sdl, err := os.ReadFile("testdata/schema-" + PinnedVersion + ".graphql")
	if err != nil {
		t.Fatal(err)
	}
	schema, gerr := gqlparser.LoadSchema(&ast.Source{Name: "suwayomi", Input: string(sdl)})
	if gerr != nil {
		t.Fatal(gerr)
	}
	for name, op := range allOps {
		if _, errs := gqlparser.LoadQuery(schema, op); len(errs) > 0 {
			t.Errorf("%s: %v", name, errs)
		}
	}
}

// fakeServer answers GraphQL requests from a map of operation name -> response.
func fakeServer(t *testing.T, responses map[string]string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNGDATA"))
			return
		}
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for name, resp := range responses {
			if strings.Contains(req.Query, " "+name) || strings.Contains(req.Query, name+"(") || strings.Contains(req.Query, name+" ") {
				_, _ = io.WriteString(w, resp)
				return
			}
		}
		t.Logf("unhandled query: %s", req.Query)
		_, _ = io.WriteString(w, `{"data":{}}`)
	}))
}

func newTestModule(t *testing.T, url string) *Module {
	impl, _ := modules.Lookup(modules.KindSource, "suwayomi")
	s, err := modules.DecodeSettings(impl, map[string]any{"url": url, "manageSettings": false, "extensionStores": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(modules.Deps{}, s.(*Settings))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMangaRelinksStaleID(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"FindManga": `{"data":{"mangas":{"nodes":[{"id":7}]}}}`,
		"FetchMangaAndChapters": `{"data":{"fetchMangaAndChapters":{"manga":{"id":7,"sourceId":"1","url":"/m/1","title":"T","status":"ONGOING","genre":["A"]},
			"chapters":[{"id":70,"url":"/c/1","name":"Ch.1","chapterNumber":1,"scanlator":" G ","uploadDate":"1700000000000","sourceOrder":0}]}}}`,
	})
	defer srv.Close()
	m := newTestModule(t, srv.URL)
	det, chs, err := m.Manga(context.Background(), source.MangaRef{SourceID: "1", URL: "/m/1"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if det.EngineRef != "7" || det.Status != source.StatusOngoing || len(chs) != 1 || chs[0].Scanlator != "G" || chs[0].UploadDate == nil {
		t.Fatalf("unexpected %+v %+v", det, chs)
	}
}

func TestNoChaptersIsNotAnError(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"FetchMangaAndChapters": `{"data":{"fetchMangaAndChapters":null},"errors":[{"message":"Exception while fetching data (/x) : No chapters found\n\tat trace"}]}`,
	})
	defer srv.Close()
	m := newTestModule(t, srv.URL)
	_, chs, err := m.Manga(context.Background(), source.MangaRef{SourceID: "1", URL: "/m/1", EngineRef: "3"}, true)
	if err != nil || len(chs) != 0 {
		t.Fatalf("err=%v chs=%v", err, chs)
	}
}

func TestErrorTrimming(t *testing.T) {
	srv := fakeServer(t, map[string]string{
		"FetchChapterPages": `{"data":null,"errors":[{"message":"Exception while fetching data (/fetchChapterPages) : HTTP error 403\r\n\tat a.b.c"}]}`,
	})
	defer srv.Close()
	m := newTestModule(t, srv.URL)
	_, err := m.Pages(context.Background(), source.ChapterRef{EngineRef: "5"})
	if err == nil || strings.Contains(err.Error(), "at a.b.c") || !strings.Contains(err.Error(), "HTTP error 403") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchPage(t *testing.T) {
	srv := fakeServer(t, nil)
	defer srv.Close()
	m := newTestModule(t, srv.URL)
	body, ct, err := m.FetchPage(context.Background(), source.Page{URL: "/api/v1/manga/1/chapter/0/page/0"})
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	b, _ := io.ReadAll(body)
	if ct != "image/png" || string(b) != "PNGDATA" {
		t.Fatalf("got %s %q", ct, b)
	}
}
