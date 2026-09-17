package api

import (
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/model"
)

func searchSeries(id int64, title string) SeriesResource {
	return SeriesResource{Series: model.Series{ID: id, Title: title, SortTitle: title, Language: "en", RootFolderID: 1,
		Metadata: model.SeriesMetadata{AltTitles: []string{title + " alternate"}}, AddedAt: time.Unix(id, 0)}}
}

func TestMatchesSeriesQueryScopesTitlesAndReading(t *testing.T) {
	item := searchSeries(1, "Moonlit Journey")
	item.Following = true
	item.Stats = SeriesStats{ChapterCount: 10, ReadCount: 3, MissingCount: 2}
	for _, query := range []SeriesSearchQuery{
		{Query: "moonlit"},
		{Query: "alternate"},
		{Filter: "following"},
		{Filter: "missing"},
		{Filter: "unread"},
		{Filter: "reading"},
		{RootFolderID: 1},
		{Language: "en"},
	} {
		if !matchesSeriesQuery(item, query) {
			t.Fatalf("query should match: %+v", query)
		}
	}
	for _, query := range []SeriesSearchQuery{{Query: "absent"}, {RootFolderID: 2}, {Language: "uk"}, {Filter: "completed"}} {
		if matchesSeriesQuery(item, query) {
			t.Fatalf("query should not match: %+v", query)
		}
	}
}

func TestSortSeriesSearch(t *testing.T) {
	a, b, c := searchSeries(1, "A"), searchSeries(2, "B"), searchSeries(3, "C")
	a.Stats.SizeOnDisk, b.Stats.SizeOnDisk, c.Stats.SizeOnDisk = 10, 30, 20
	items := []SeriesResource{a, b, c}
	sortSeriesSearch(items, "size")
	if items[0].Title != "B" || items[1].Title != "C" || items[2].Title != "A" {
		t.Fatalf("size order: %s %s %s", items[0].Title, items[1].Title, items[2].Title)
	}
	sortSeriesSearch(items, "title")
	if items[0].Title != "A" || items[2].Title != "C" {
		t.Fatalf("title order: %s %s %s", items[0].Title, items[1].Title, items[2].Title)
	}
}
