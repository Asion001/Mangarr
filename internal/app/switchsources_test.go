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

// TestSwitchSourceModule moves a library from one source module to another,
// which is how a Suwayomi library moves onto mangarr's own sites: catalogs
// both modules know by the same id are re-pointed, the rest stay put.
func TestSwitchSourceModule(t *testing.T) {
	// the old engine carries two catalogs, the new one only the first
	old := fakesource.NewScenario("switch-old")
	old.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}, {ID: "Gone", Name: "Source Gone", Lang: "en"}}
	newer := fakesource.NewScenario("switch-new")
	newer.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}}
	for _, sc := range []*fakesource.Scenario{old, newer} {
		for _, src := range sc.Sources {
			sc.AddManga(&fakesource.Manga{SourceID: src.ID, URL: "/" + src.ID + "/switch", Title: "Switch Me",
				Status:   source.StatusOngoing,
				Chapters: []fakesource.Chapter{{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now()}}})
		}
	}

	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	from := e.addFakeModule(t, "switch-old")
	to := e.addFakeModule(t, "switch-new")
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Switch Me", RootFolderID: e.RFID, Monitor: model.MonitorNone, NoRefresh: true,
		Sources: []series.SourceLink{
			{ModuleID: from, SourceID: "A", URL: "/A/switch", SourceName: "Source A", Lang: "en"},
			{ModuleID: from, SourceID: "Gone", URL: "/Gone/switch", SourceName: "Source Gone", Lang: "en"},
		}})
	if err != nil {
		t.Fatal(err)
	}
	// an engine ref only the old module could make sense of
	if _, err := e.App.DB.NewUpdate().Model((*model.SeriesSource)(nil)).Set("engine_ref = ?", "77").
		Where("series_id = ?", ser.ID).Exec(e.Ctx); err != nil {
		t.Fatal(err)
	}

	req := series.SwitchRequest{FromModuleID: from, ToModuleID: to}
	rows, err := e.App.Series.SwitchPlan(e.Ctx, req)
	if err != nil || len(rows) != 2 {
		t.Fatalf("plan: %v %+v", err, rows)
	}
	if !rows[0].Moves || rows[0].SourceID != "A" || rows[0].Series != 1 {
		t.Fatalf("the catalog both modules have: %+v", rows[0])
	}
	if rows[1].Moves || rows[1].Reason == "" {
		t.Fatalf("the catalog only the old module has: %+v", rows[1])
	}
	if got := series.SwitchSummary(rows); got != "1 links move, 1 stay" {
		t.Fatalf("summary %q", got)
	}
	// a plan changes nothing
	if n, _ := e.App.DB.NewSelect().Model((*model.SeriesSource)(nil)).Where("module_id = ?", to).Count(e.Ctx); n != 0 {
		t.Fatalf("the plan moved %d links", n)
	}

	moved, err := e.App.Series.SwitchSources(e.Ctx, req, nil)
	if err != nil || moved != 1 {
		t.Fatalf("switch: %v moved %d", err, moved)
	}
	var links []model.SeriesSource
	_ = e.App.DB.NewSelect().Model(&links).Where("series_id = ?", ser.ID).Order("source_id").Scan(e.Ctx)
	if len(links) != 2 {
		t.Fatalf("links %+v", links)
	}
	if links[0].ModuleID != to || links[0].EngineRef != "" {
		t.Fatalf("the moved link kept an engine ref from the old module: %+v", links[0])
	}
	if links[1].ModuleID != from {
		t.Fatalf("a catalog the new module doesn't have should stay: %+v", links[1])
	}

	// running it again has nothing left to move
	if again, err := e.App.Series.SwitchSources(e.Ctx, req, nil); err != nil || again != 0 {
		t.Fatalf("a second run moved %d: %v", again, err)
	}
	// and the series still refreshes, now through the new module
	res, err := e.App.Refresher.SyncSeries(e.Ctx, ser.ID, false)
	if err != nil || res.NewChapters == 0 {
		t.Fatalf("refresh after the switch: %v %+v", err, res)
	}
}
