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

// TestBulkReplaceSources: a library moves from one catalog to another; each
// series is found at the new one and swapped in place, and a reviewed pick
// fixes the series the search can't find.
func TestBulkReplaceSources(t *testing.T) {
	sc := fakesource.NewScenario("bulk-replace")
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}, {ID: "B", Name: "Source B", Lang: "en"}}
	for _, title := range []string{"Moving One", "Moving Two"} {
		for _, src := range []string{"A", "B"} {
			sc.AddManga(&fakesource.Manga{SourceID: src, URL: "/" + src + "/" + title, Title: title, Status: source.StatusOngoing,
				Chapters: []fakesource.Chapter{{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now()}}})
		}
	}
	// B knows the third series under a title no search would match
	sc.AddManga(&fakesource.Manga{SourceID: "B", URL: "/B/renamed", Title: "Completely Different Name", Status: source.StatusOngoing})
	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	mod := e.addFakeModule(t, "bulk-replace")

	var ids []int64
	for _, title := range []string{"Moving One", "Moving Two", "Hard To Find"} {
		ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: title, RootFolderID: e.RFID, Monitor: model.MonitorNone, NoRefresh: true,
			Sources: []series.SourceLink{{ModuleID: mod, SourceID: "Z", URL: "/Z/" + title, SourceName: "Other", Lang: "en"},
				{ModuleID: mod, SourceID: "A", URL: "/A/" + title, SourceName: "Source A", Lang: "en"}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, ser.ID)
	}
	req := series.BulkRequest{Action: series.BulkReplace, ModuleID: mod, SourceID: "B", FromModuleID: mod, FromSourceID: "A", SeriesIDs: ids}

	preview, err := e.App.Series.BulkSources(e.Ctx, req, true, nil)
	if err != nil || len(preview) != 3 {
		t.Fatalf("preview: %v %+v", err, preview)
	}
	if preview[0].Done != "replaced" && preview[1].Done != "replaced" {
		t.Fatalf("preview: %+v", preview)
	}
	for _, r := range preview {
		if r.SeriesID == ids[2] && r.Done != "skipped" {
			t.Fatalf("the unfindable series should need a pick: %+v", r)
		}
	}

	// apply with a pick for the third
	req.Picks = []series.BulkPick{{SeriesID: ids[2], URL: "/B/renamed", Title: "Completely Different Name"}}
	res, err := e.App.Series.BulkSources(e.Ctx, req, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Done != "replaced" {
			t.Fatalf("not replaced: %+v", r)
		}
	}
	for _, id := range ids {
		var links []model.SeriesSource
		_ = e.App.DB.NewSelect().Model(&links).Where("series_id = ?", id).Order("priority", "id").Scan(e.Ctx)
		if len(links) != 2 || links[0].SourceID != "Z" || links[1].SourceID != "B" {
			t.Fatalf("series %d: want Z then B in A's place, got %+v", id, links)
		}
	}
}
