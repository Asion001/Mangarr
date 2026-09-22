package app_test

import (
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

// Bulk edit refreshes and searches several series with one command each.
func TestRefreshAndSearchSeveralSeries(t *testing.T) {
	sc := fakesource.NewScenario("bulk-commands")
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	for _, m := range []struct{ url, title string }{{"/one", "First Series"}, {"/two", "Second Series"}} {
		sc.AddManga(&fakesource.Manga{SourceID: "A", URL: m.url, Title: m.title, Status: source.StatusOngoing,
			Chapters: []fakesource.Chapter{{URL: m.url + "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now()}}})
	}
	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	mod := e.addFakeModule(t, "bulk-commands")
	var ids []int64
	for _, m := range []struct{ url, title string }{{"/one", "First Series"}, {"/two", "Second Series"}} {
		ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: m.title, RootFolderID: e.RFID, Monitor: model.MonitorAll,
			Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: m.url, SourceName: "Source A", Lang: "en"}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, ser.ID)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesIds": ids})
	for _, id := range ids {
		n, err := e.App.DB.NewSelect().Model((*model.Chapter)(nil)).Where("series_id = ?", id).Count(e.Ctx)
		if err != nil || n != 1 {
			t.Fatalf("series %d: want 1 chapter after refresh, got %d (%v)", id, n, err)
		}
	}
	e.runCommand(t, "SearchMissing", map[string]any{"seriesIds": ids})
	waitFor(t, 20*time.Second, "both chapters downloaded", func() bool {
		return len(e.chapterFiles(t, ids[0])) == 1 && len(e.chapterFiles(t, ids[1])) == 1
	})
}
