package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

// fakeCatalog is one fake module whose pages are w px wide.
func fakeCatalog(t *testing.T, e *testEnv, name string, w int) int64 {
	t.Helper()
	sc := fakesource.NewScenario(name)
	sc.PageWidth = w
	sc.Sources = []source.SourceInfo{{ID: name, Name: name, Lang: "en"}}
	sc.AddManga(&fakesource.Manga{SourceID: name, URL: "/m", Title: "Rules", Status: source.StatusOngoing,
		Chapters: []fakesource.Chapter{{URL: "/c1", Name: "Chapter 1", Number: 1, Uploaded: time.Now(), Pages: 3}}})
	return e.addFakeModule(t, name)
}

func addWith(t *testing.T, e *testEnv, title string, profileID int64, mods map[int64]string, order []int64) *model.Series {
	t.Helper()
	var links []series.SourceLink
	for _, id := range order {
		links = append(links, series.SourceLink{ModuleID: id, SourceID: mods[id], URL: "/m", SourceName: mods[id], Lang: "en"})
	}
	ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: title, RootFolderID: e.RFID, ProfileID: profileID, Monitor: model.MonitorAll,
		SearchMissing: true, Sources: links})
	if err != nil {
		t.Fatal(err)
	}
	e.runCommand(t, "RefreshSeries", map[string]any{"seriesId": ser.ID})
	return ser
}

func blocklistReasons(t *testing.T, e *testEnv, seriesID int64) string {
	t.Helper()
	var h []model.History
	_ = e.App.DB.NewSelect().Model(&h).Where("series_id = ? AND event_type = ?", seriesID, model.HistoryBlocklist).Scan(e.Ctx)
	var out []string
	for _, x := range h {
		out = append(out, x.Data["reason"])
	}
	return strings.Join(out, "; ")
}

// TestJunkOnlyChapterTriesTheNextSource: a source whose chapter is nothing
// but tiny images is blocklisted and the next source's pages are imported.
func TestJunkOnlyChapterTriesTheNextSource(t *testing.T) {
	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	junk := fakeCatalog(t, e, "junk-only", 60)
	real := fakeCatalog(t, e, "real-pages", 200)
	ser := addWith(t, e, "Junk First", 0, map[int64]string{junk: "junk-only", real: "real-pages"}, []int64{junk, real})
	waitFor(t, 30*time.Second, "download from the second source", func() bool { return len(e.chapterFiles(t, ser.ID)) == 1 })
	if f := e.chapterFiles(t, ser.ID)["1"]; f.AvgWidth != 200 {
		t.Fatalf("imported the wrong release: %+v\n%s", f, jobsAndHistory(t, e, ser.ID))
	}
	if r := blocklistReasons(t, e, ser.ID); !strings.Contains(r, "only junk images") {
		t.Fatalf("junk release should be blocklisted with a reason, got %q", r)
	}
}

// TestLowResolutionReleases: with "try another source" a narrow release is
// replaced by a wider one when there is one, and kept when it's the only one.
func TestLowResolutionReleases(t *testing.T) {
	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	var prof model.Profile
	_ = e.App.DB.NewSelect().Model(&prof).Where("is_default = ?", true).Scan(e.Ctx)
	prof.Config.LowRes = model.LowResRule{Width: 720, Action: model.LowResRetry}
	if _, err := e.App.DB.NewUpdate().Model(&prof).WherePK().Exec(e.Ctx); err != nil {
		t.Fatal(err)
	}
	low := fakeCatalog(t, e, "low-res", 300)
	high := fakeCatalog(t, e, "high-res", 800)

	both := addWith(t, e, "Low First", prof.ID, map[int64]string{low: "low-res", high: "high-res"}, []int64{low, high})
	waitFor(t, 30*time.Second, "wider release", func() bool { return len(e.chapterFiles(t, both.ID)) == 1 })
	if f := e.chapterFiles(t, both.ID)["1"]; f.AvgWidth != 800 {
		t.Fatalf("should have tried the wider source: %+v\n%s", f, jobsAndHistory(t, e, both.ID))
	}
	if r := blocklistReasons(t, e, both.ID); !strings.Contains(r, "low resolution") {
		t.Fatalf("low-res release should be blocklisted with a reason, got %q", r)
	}

	only := addWith(t, e, "Low Only", prof.ID, map[int64]string{low: "low-res"}, []int64{low})
	waitFor(t, 30*time.Second, "the only release, kept", func() bool { return len(e.chapterFiles(t, only.ID)) == 1 })
	if f := e.chapterFiles(t, only.ID)["1"]; f.AvgWidth != 300 {
		t.Fatalf("the only release should be kept: %+v", f)
	}
}
