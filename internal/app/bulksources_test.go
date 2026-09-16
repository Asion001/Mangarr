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

// TestBulkSources: a catalog is added to a whole library as a fallback
// source, found by title, and lands behind the sources a series already has.
func TestBulkSources(t *testing.T) {
	sc := fakesource.NewScenario("bulk")
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "Source A", Lang: "en"}, {ID: "B", Name: "Source B", Lang: "en"}}
	for _, title := range []string{"Bulk One", "Bulk Two"} {
		for _, src := range []string{"A", "B"} {
			sc.AddManga(&fakesource.Manga{SourceID: src, URL: "/" + src + "/" + title, Title: title, Status: source.StatusOngoing,
				Chapters: []fakesource.Chapter{{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now()}}})
		}
	}
	// a series nothing matches
	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	mod := e.addFakeModule(t, "bulk")

	var ids []int64
	for _, title := range []string{"Bulk One", "Bulk Two"} {
		ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: title, RootFolderID: e.RFID, Monitor: model.MonitorNone, NoRefresh: true,
			Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/A/" + title, SourceName: "Source A", Lang: "en"}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, ser.ID)
	}
	lonely, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: "Nothing Has This", RootFolderID: e.RFID, Monitor: model.MonitorNone, NoRefresh: true,
		Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/A/none", SourceName: "Source A", Lang: "en"}}})
	if err != nil {
		t.Fatal(err)
	}
	ids = append(ids, lonely.ID)

	req := series.BulkRequest{Action: series.BulkAdd, ModuleID: mod, SourceID: "B", SeriesIDs: ids}

	// a dry run says what would happen and changes nothing
	preview, err := e.App.Series.BulkSources(e.Ctx, req, true, nil)
	if err != nil || len(preview) != 3 {
		t.Fatalf("preview: %v %+v", err, preview)
	}
	added, skipped := count(preview)
	if added != 2 || skipped != 1 {
		t.Fatalf("preview says %d added, %d skipped: %+v", added, skipped, preview)
	}
	if n, _ := e.App.DB.NewSelect().Model((*model.SeriesSource)(nil)).Where("source_id = ?", "B").Count(e.Ctx); n != 0 {
		t.Fatalf("the dry run linked %d sources", n)
	}

	// applying it links the catalog behind the source each series already has
	done := 0
	results, err := e.App.Series.BulkSources(e.Ctx, req, false, func(d, total int) { done = d })
	if err != nil {
		t.Fatal(err)
	}
	if added, skipped := count(results); added != 2 || skipped != 1 || done != 3 {
		t.Fatalf("applied %d added %d skipped (progress %d): %+v", added, skipped, done, results)
	}
	if got := series.BulkSummary(results); got != "2 added, 1 skipped" {
		t.Fatalf("summary %q", got)
	}
	var links []model.SeriesSource
	_ = e.App.DB.NewSelect().Model(&links).Where("series_id = ?", ids[0]).Order("priority").Scan(e.Ctx)
	if len(links) != 2 || links[0].SourceID != "A" || links[1].SourceID != "B" || links[1].Priority <= links[0].Priority {
		t.Fatalf("sources after adding a fallback: %+v", links)
	}

	// running it again changes nothing
	again, err := e.App.Series.BulkSources(e.Ctx, req, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if added, _ := count(again); added != 0 {
		t.Fatalf("a second run added %d: %+v", added, again)
	}

	// switching the fallback off, then removing it
	off := series.BulkRequest{Action: series.BulkDisable, ModuleID: mod, SourceID: "B", SeriesIDs: ids}
	if res, err := e.App.Series.BulkSources(e.Ctx, off, false, nil); err != nil || countDone(res, "disabled") != 2 {
		t.Fatalf("disable: %v %+v", err, res)
	}
	var one model.SeriesSource
	_ = e.App.DB.NewSelect().Model(&one).Where("series_id = ? AND source_id = ?", ids[0], "B").Scan(e.Ctx)
	if one.Enabled {
		t.Fatal("the fallback is still enabled")
	}
	rm := series.BulkRequest{Action: series.BulkRemove, ModuleID: mod, SourceID: "B", SeriesIDs: ids}
	if res, err := e.App.Series.BulkSources(e.Ctx, rm, false, nil); err != nil || countDone(res, "removed") != 2 {
		t.Fatalf("remove: %v %+v", err, res)
	}
	if n, _ := e.App.DB.NewSelect().Model((*model.SeriesSource)(nil)).Where("source_id = ?", "B").Count(e.Ctx); n != 0 {
		t.Fatalf("%d links left", n)
	}
}

func count(rs []series.BulkResult) (added, skipped int) {
	return countDone(rs, "added"), countDone(rs, "skipped")
}

func countDone(rs []series.BulkResult, done string) int {
	n := 0
	for _, r := range rs {
		if r.Done == done {
			n++
		}
	}
	return n
}
