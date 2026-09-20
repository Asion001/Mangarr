package history

import (
	"context"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
)

func TestListFiltersAndSorts(t *testing.T) {
	dbtest.ForEachDialect(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		now := time.Now().UTC()
		root := &model.RootFolder{Path: "/library", Language: "en", CreatedAt: now}
		profile := &model.Profile{Name: "Default", IsDefault: true, Config: model.ProfileConfig{}, CreatedAt: now, UpdatedAt: now}
		if _, err := d.NewInsert().Model(root).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := d.NewInsert().Model(profile).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		addSeries := func(title string) *model.Series {
			s := &model.Series{Title: title, SortTitle: title, Status: "ongoing", Monitored: true, MonitorNew: model.MonitorAll,
				RootFolderID: root.ID, Path: title, ProfileID: profile.ID, Language: "en", SourcePriorityMode: "custom",
				ReadingDirection: "rtl", Tags: []int64{}, Metadata: model.SeriesMetadata{}, AddOptions: model.AddOptions{},
				BlockedScanlators: []string{}, AddedAt: now, UpdatedAt: now}
			if _, err := d.NewInsert().Model(s).Exec(ctx); err != nil {
				t.Fatal(err)
			}
			return s
		}
		alpha, zulu := addSeries("Alpha"), addSeries("Zulu")
		if err := Record(ctx, d, alpha.ID, nil, model.HistoryImported, "Alpha chapter", nil); err != nil {
			t.Fatal(err)
		}
		if err := Record(ctx, d, zulu.ID, nil, model.HistoryFailed, "Zulu chapter", nil); err != nil {
			t.Fatal(err)
		}

		assertFirst := func(q Query, want int64) {
			t.Helper()
			page, err := List(ctx, d, q)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) == 0 || page.Items[0].SeriesID != want {
				t.Fatalf("query %+v returned %+v, want series %d first", q, page.Items, want)
			}
		}
		assertFirst(Query{}, zulu.ID)
		assertFirst(Query{Sort: "oldest"}, alpha.ID)
		assertFirst(Query{Sort: "series"}, alpha.ID)
		assertFirst(Query{Sort: "event"}, zulu.ID)
		assertFirst(Query{SeriesID: alpha.ID}, alpha.ID)
	})
}
