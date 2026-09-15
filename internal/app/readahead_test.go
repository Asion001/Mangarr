package app_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

// TestReadAhead: reading drives downloads. Finishing chapter 1 in an app
// monitors and downloads chapters 2-4 (readAhead.chapters = 3) of a series
// that isn't monitored; chapter 5 stays as it was.
func TestReadAhead(t *testing.T) {
	for dialect, dsn := range dbtest.DSNs(t) {
		t.Run(dialect, func(t *testing.T) {
			sc := fakesource.NewScenario("readahead-" + dialect)
			sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
			var chs []fakesource.Chapter
			for i := 1; i <= 5; i++ {
				chs = append(chs, fakesource.Chapter{URL: "/c" + strconv.Itoa(i), Name: "Chapter " + strconv.Itoa(i), Number: float64(i), Uploaded: time.Now().Add(-time.Hour)})
			}
			sc.AddManga(&fakesource.Manga{SourceID: "A", URL: "/m", Title: "Ahead", Status: source.StatusOngoing, Chapters: chs})
			e := newTestApp(t, dsn)
			e.App.ReadAhead.SetDelay(20 * time.Millisecond)
			mod := e.addFakeModule(t, "readahead-"+dialect)
			ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Ahead", RootFolderID: e.RFID, Monitor: model.MonitorNone,
				Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/m", SourceName: "Source A", Lang: "en"}}})
			if err != nil {
				t.Fatal(err)
			}
			e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
			var list []model.Chapter
			_ = e.App.DB.NewSelect().Model(&list).Where("series_id = ?", ser.ID).Order("number_sort").Scan(e.Ctx)
			if len(list) != 5 {
				t.Fatalf("chapters %d", len(list))
			}
			key, _, _ := e.App.Komga.CreateKey(e.Ctx, "test", "test")
			srv := httptest.NewServer(e.App.Komga.Handler())
			defer srv.Close()
			req, _ := http.NewRequest("PATCH", srv.URL+"/api/v1/books/"+sid(list[0].ID)+"/read-progress", strings.NewReader(`{"completed":true}`))
			req.Header.Set("X-API-Key", key)
			if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 204 {
				t.Fatalf("patch: %v", err)
			}
			waitFor(t, 20*time.Second, "chapters 2-4 downloaded", func() bool { return len(e.chapterFiles(t, ser.ID)) == 3 })
			files := e.chapterFiles(t, ser.ID)
			for _, n := range []string{"2", "3", "4"} {
				if _, ok := files[n]; !ok {
					t.Fatalf("chapter %s not downloaded: %v", n, files)
				}
			}
			var ch5 model.Chapter
			_ = e.App.DB.NewSelect().Model(&ch5).Where("id = ?", list[4].ID).Scan(e.Ctx)
			if ch5.Monitored || ch5.FileID != nil {
				t.Fatalf("chapter 5 touched: %+v", ch5)
			}
			n, _ := e.App.DB.NewSelect().Model((*model.History)(nil)).Where("series_id = ? AND event_type = ?", ser.ID, model.HistoryReadAhead).Count(e.Ctx)
			if n != 1 {
				t.Fatalf("readAhead history: %d", n)
			}
		})
	}
}
