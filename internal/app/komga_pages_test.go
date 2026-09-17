package app_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

// TestKomgaAPIPages reads an undownloaded chapter (streamed from the source,
// which queues its download), then the same chapter from its CBZ.
func TestKomgaAPIPages(t *testing.T) {
	for dialect, dsn := range dbtest.DSNs(t) {
		t.Run(dialect, func(t *testing.T) {
			sc := fakesource.NewScenario("komga-pages-" + dialect)
			sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
			t0 := time.Now().Add(-48 * time.Hour)
			sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/a/1", Title: "Streamed", Status: source.StatusOngoing, Chapters: []fakesource.Chapter{
				{URL: "/a/1/c1", Name: "Chapter 1", Number: 1, Uploaded: t0},
				{URL: "/a/1/c2", Name: "Chapter 2", Number: 2, Uploaded: t0, Pages: 4},
			}})
			e := newTestApp(t, dsn)
			mod := e.addFakeModule(t, "komga-pages-"+dialect)
			ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Streamed", RootFolderID: e.RFID, Monitor: model.MonitorNone,
				Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/a/1", SourceName: "Source A", Lang: "en"}}})
			if err != nil {
				t.Fatal(err)
			}
			e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
			var chs []model.Chapter
			_ = e.App.DB.NewSelect().Model(&chs).Where("series_id = ?", ser.ID).Order("number_sort").Scan(e.Ctx)
			if len(chs) != 2 || chs[0].FileID != nil {
				t.Fatalf("chapters %+v", chs)
			}
			key, _, err := e.App.Komga.CreateKey(e.Ctx, 0, "test", "test")
			if err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(e.App.Komga.Handler())
			defer srv.Close()
			get := func(path string) *http.Response {
				t.Helper()
				req, _ := http.NewRequest("GET", srv.URL+path, nil)
				req.Header.Set("X-API-Key", key)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				return resp
			}
			getJSON := func(path string, out any) {
				t.Helper()
				resp := get(path)
				defer resp.Body.Close()
				if resp.StatusCode != 200 {
					body, _ := io.ReadAll(resp.Body)
					t.Fatalf("%s: %d %s", path, resp.StatusCode, body)
				}
				_ = json.NewDecoder(resp.Body).Decode(out)
			}
			c1, c2 := sid(chs[0].ID), sid(chs[1].ID)

			// streamed: the source's page list, then an image
			var pages []map[string]any
			getJSON("/api/v1/books/"+c1+"/pages", &pages)
			if len(pages) != 3 || pages[0]["number"] != 1.0 || pages[0]["mediaType"] == "" || pages[0]["fileName"] == "" {
				t.Fatalf("pages %v", pages)
			}
			resp := get("/api/v1/books/" + c1 + "/pages/1")
			img, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || len(img) == 0 {
				t.Fatalf("page: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			resp = get("/api/v1/books/" + c1 + "/pages/1?convert=jpeg")
			if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" {
				t.Fatalf("convert: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			if resp := get("/api/v1/books/" + c1 + "/pages/9"); resp.StatusCode != 404 {
				t.Fatalf("page out of range: %d", resp.StatusCode)
			}
			if resp := get("/api/v1/books/" + c1 + "/pages/1/thumbnail"); resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" &&
				resp.Header.Get("Content-Type") != "image/png" {
				t.Fatalf("page thumbnail: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
			}
			var book map[string]any
			getJSON("/api/v1/books/"+c1, &book)
			if book["media"].(map[string]any)["pagesCount"] != 3.0 {
				t.Fatalf("page count after streaming: %v", book["media"])
			}

			// opening it queued the download; afterwards it's read from the file
			waitFor(t, 20*time.Second, "download on open", func() bool { return len(e.chapterFiles(t, ser.ID)) == 1 })
			getJSON("/api/v1/books/"+c1, &book)
			if !strings.HasSuffix(book["url"].(string), ".cbz") {
				t.Fatalf("url after download %v", book["url"])
			}
			getJSON("/api/v1/books/"+c1+"/pages", &pages)
			if len(pages) != 3 || pages[0]["sizeBytes"] == nil {
				t.Fatalf("pages from file %v", pages)
			}
			fetches := sc.FetchCount()
			resp = get("/api/v1/books/" + c1 + "/pages/2")
			if resp.StatusCode != 200 || sc.FetchCount() != fetches || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
				t.Fatalf("page from file: %d %s fetches %d→%d", resp.StatusCode, resp.Header.Get("Content-Type"), fetches, sc.FetchCount())
			}
			resp = get("/api/v1/books/" + c1 + "/file")
			data, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), ".cbz") {
				t.Fatalf("file: %d %q", resp.StatusCode, resp.Header.Get("Content-Disposition"))
			}
			if zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data))); err != nil || len(zr.File) < 3 {
				t.Fatalf("file is not the CBZ: %v", err)
			}
			if resp := get("/api/v1/books/" + c1 + "/thumbnail"); resp.StatusCode != 200 {
				t.Fatalf("book thumbnail: %d", resp.StatusCode)
			}

			// with downloadOnOpen off, reading streams without queuing
			rc := settings.DefaultReading()
			rc.DownloadOnOpen = false
			if err := e.App.Settings.Set(e.Ctx, settings.KeyReading, rc); err != nil {
				t.Fatal(err)
			}
			getJSON("/api/v1/books/"+c2+"/pages", &pages)
			if len(pages) != 4 {
				t.Fatalf("pages of chapter 2: %d", len(pages))
			}
			// (no download runs now, so the fetch count is stable)
			if resp := get("/api/v1/books/" + c2 + "/pages/1"); resp.StatusCode != 200 {
				t.Fatalf("chapter 2 page: %d", resp.StatusCode)
			}
			fetches = sc.FetchCount()
			if resp := get("/api/v1/books/" + c2 + "/pages/1?convert=png"); resp.StatusCode != 200 || sc.FetchCount() != fetches {
				t.Fatalf("a streamed page is fetched once (%d fetches, then %d)", fetches, sc.FetchCount())
			}
			n, _ := e.App.DB.NewSelect().Model((*model.DownloadJob)(nil)).Where("chapter_id = ?", chs[1].ID).Count(e.Ctx)
			if n != 0 {
				t.Fatalf("downloadOnOpen off still queued %d jobs", n)
			}
			if resp := get("/api/v1/books/" + c2 + "/file"); resp.StatusCode != 404 {
				t.Fatalf("file of an undownloaded chapter: %d", resp.StatusCode)
			}
		})
	}
}

// TestStreamedPageFetchedOnce: twenty readers opening the same page of a
// chapter nobody has downloaded ask the source for it once. Without that,
// a cold chapter opened by a few devices is a burst at the site.
func TestStreamedPageFetchedOnce(t *testing.T) {
	sc := fakesource.NewScenario("stream-once")
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	sc.PageDelay = 150 * time.Millisecond // long enough for the others to pile up
	sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/a/1", Title: "Cold", Status: source.StatusOngoing,
		Chapters: []fakesource.Chapter{{URL: "/a/1/c1", Name: "Chapter 1", Number: 1, Pages: 2, Uploaded: time.Now()}}})

	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	mod := e.addFakeModule(t, "stream-once")
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Cold", RootFolderID: e.RFID, Monitor: model.MonitorNone,
		Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/a/1", SourceName: "Source A", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
	var ch model.Chapter
	if err := e.App.DB.NewSelect().Model(&ch).Where("series_id = ?", ser.ID).Limit(1).Scan(e.Ctx); err != nil {
		t.Fatal(err)
	}
	key, _, err := e.App.Komga.CreateKey(e.Ctx, 0, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(e.App.Komga.Handler())
	defer srv.Close()

	// opening a chapter also queues it; keep the download out of the count
	e.App.Downloads.Hold(true)
	defer e.App.Downloads.Hold(false)
	sc.Fetches = 0
	var wg sync.WaitGroup
	var bad atomic.Int32
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest("GET", srv.URL+"/api/v1/books/"+sid(ch.ID)+"/pages/1", nil)
			req.Header.Set("X-API-Key", key)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				bad.Add(1)
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 || len(body) == 0 {
				bad.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := bad.Load(); n != 0 {
		t.Fatalf("%d readers didn't get the page", n)
	}
	if sc.Fetches != 1 {
		t.Fatalf("the source was asked for the same page %d times", sc.Fetches)
	}
}
