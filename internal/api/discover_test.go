package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/api"
	"github.com/Asion001/mangarr/internal/catalogs"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

func TestDiscoverAggregatesPersonalLibraryAndCachedSources(t *testing.T) {
	srv, app := newServer(t, true)
	ctx := context.Background()
	now := time.Now().UTC()
	root := &model.RootFolder{Path: t.TempDir(), Language: "en", CreatedAt: now}
	if _, err := app.DB.NewInsert().Model(root).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	var profile model.Profile
	if err := app.DB.NewSelect().Model(&profile).Order("id").Limit(1).Scan(ctx); err != nil {
		t.Fatal(err)
	}

	addSeries := func(title string, genres []string, changed time.Time) (*model.Series, *model.Chapter) {
		t.Helper()
		series := &model.Series{Title: title, SortTitle: strings.ToLower(title), Status: model.StatusOngoing, Monitored: true,
			MonitorNew: model.MonitorAll, RootFolderID: root.ID, Path: title, ProfileID: profile.ID, Language: "en",
			SourcePriorityMode: "inherit", ReadingDirection: "rtl", Tags: []int64{}, Metadata: model.SeriesMetadata{Genres: genres},
			AddedAt: changed, UpdatedAt: changed}
		if _, err := app.DB.NewInsert().Model(series).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		chapter := &model.Chapter{SeriesID: series.ID, NumberKey: "12", NumberSort: 12, Title: "Update", Monitored: true,
			State: model.ChapterMissing, FirstSeenAt: changed, UpdatedAt: changed, ReleaseDate: &changed}
		if _, err := app.DB.NewInsert().Model(chapter).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		return series, chapter
	}
	readSeries, readChapter := addSeries("Read Action", []string{"Action"}, now.Add(-3*time.Hour))
	recommended, _ := addSeries("Suggested Action", []string{"Action", "Adventure"}, now.Add(-2*time.Hour))
	recent, _ := addSeries("Recent Drama", []string{"Drama"}, now.Add(-time.Hour))
	readerID, err := app.Reading.ReaderID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	readAt := now.Add(-2 * time.Hour)
	if _, err := app.DB.NewInsert().Model(&model.ChapterReadState{ReaderID: readerID, ChapterID: readChapter.ID,
		SeriesID: readSeries.ID, Completed: true, ReadAt: &readAt, SyncedAt: readAt}).Exec(ctx); err != nil {
		t.Fatal(err)
	}

	scenario := fakesource.NewScenario("discover")
	scenario.Sources = []source.SourceInfo{
		{ID: "A", Name: "Alpha", DisplayName: "Alpha (EN)", Lang: "en", SupportsLatest: true},
		{ID: "B", Name: "Beta", DisplayName: "Beta (EN)", Lang: "en", SupportsLatest: true},
	}
	scenario.AddManga(&fakesource.Manga{SourceID: "A", URL: "/suggested", Title: recommended.Title})
	scenario.AddManga(&fakesource.Manga{SourceID: "A", URL: "/new", Title: "New Source Title"})
	scenario.AddManga(&fakesource.Manga{SourceID: "B", URL: "/duplicate", Title: "New Source Title"})
	definition := &model.ProviderDefinition{Kind: "source", Implementation: "fake", Name: "Discover fake", Enabled: true,
		Settings: map[string]any{"scenario": "discover"}}
	if err := app.Modules.Create(ctx, definition); err != nil {
		t.Fatal(err)
	}

	get := func() api.DiscoverResponse {
		t.Helper()
		var response api.DiscoverResponse
		if code := doJSON(t, http.MethodGet, srv.URL+"/api/v1/discover?lang=en&limit=10", "", &response); code != http.StatusOK {
			t.Fatalf("discover: %d", code)
		}
		return response
	}
	first := get()
	if len(first.Recommendations) < 2 || first.Recommendations[0].SeriesID != recommended.ID || first.Recommendations[0].Reason != "matches-genres" {
		t.Fatalf("personal recommendations: %+v", first.Recommendations)
	}
	if len(first.Updates) != 3 || first.Updates[0].SeriesID != recent.ID || first.Updates[0].LatestChapter != "12" {
		t.Fatalf("recent updates: %+v", first.Updates)
	}
	if len(first.Popular) != 2 || first.PopularCached {
		t.Fatalf("popular aggregation: %+v cached=%v", first.Popular, first.PopularCached)
	}
	foundExisting := false
	for _, item := range first.Popular {
		foundExisting = foundExisting || item.ExistingSeriesID == recommended.ID
	}
	if !foundExisting {
		t.Fatalf("popular title was not matched to the existing series: %+v", first.Popular)
	}
	if first.Popular[0].ThumbnailURL == "" {
		t.Fatal("discover result has no signed thumbnail")
	}
	if code := doJSON(t, http.MethodGet, srv.URL+first.Popular[0].ThumbnailURL, "", nil); code != http.StatusOK {
		t.Fatalf("signed thumbnail: %d", code)
	}
	if code := doJSON(t, http.MethodGet, srv.URL+first.Popular[0].ThumbnailURL+"x", "", nil); code != http.StatusNotFound {
		t.Fatalf("tampered thumbnail token: %d", code)
	}
	second := get()
	if !second.PopularCached {
		t.Fatal("second discover request should reuse every source page")
	}
	scenario.Update(func() {
		if scenario.Browses["A"] != 1 || scenario.Browses["B"] != 1 {
			t.Fatalf("browse cache calls: %v", scenario.Browses)
		}
		scenario.BrowseErr["B"] = context.DeadlineExceeded
	})
	app.SourceCache.Clear()
	partial := get()
	if len(partial.SourceErrors) != 1 || partial.SourceErrors[0].Source != catalogs.Key(definition.ID, "B") || len(partial.Popular) != 2 {
		t.Fatalf("partial source response: errors=%+v popular=%+v", partial.SourceErrors, partial.Popular)
	}
}
