package app_test

import (
	"testing"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/sourcepriority"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

func TestInheritedSourcePriorityAndCustomSnapshot(t *testing.T) {
	for dialect, dsn := range dbtest.DSNs(t) {
		t.Run(dialect, func(t *testing.T) {
			scenario := "source-priority-" + dialect
			sc := fakesource.NewScenario(scenario)
			sc.Sources = []source.SourceInfo{
				{ID: "A", Name: "Source A", Lang: "uk"},
				{ID: "B", Name: "Source B", Lang: "uk"},
				{ID: "C", Name: "Source C", Lang: "uk"},
			}
			e := newTestApp(t, dsn)
			mod := e.addFakeModule(t, scenario)
			ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{
				Title: "Priority Test", RootFolderID: e.RFID, Language: "uk", Monitor: model.MonitorNone, NoRefresh: true,
				Sources: []series.SourceLink{
					{ModuleID: mod, SourceID: "A", URL: "/a", SourceName: "Source A", Lang: "uk"},
					{ModuleID: mod, SourceID: "B", URL: "/b", SourceName: "Source B", Lang: "uk"},
					{ModuleID: mod, SourceID: "C", URL: "/c", SourceName: "Source C", Lang: "uk"},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			lists := []model.SourcePriorityList{
				{Scope: sourcepriority.LibraryScope(e.RFID), Sources: []string{sourcepriority.Key(mod, "C")}},
				{Scope: sourcepriority.LanguageScope("uk"), Sources: []string{sourcepriority.Key(mod, "B")}},
			}
			for i := range lists {
				if _, err := e.App.DB.NewInsert().Model(&lists[i]).Exec(e.Ctx); err != nil {
					t.Fatal(err)
				}
			}

			assertOrder := func(want string) {
				t.Helper()
				var links []model.SeriesSource
				if err := e.App.DB.NewSelect().Model(&links).Where("series_id = ?", ser.ID).Scan(e.Ctx); err != nil {
					t.Fatal(err)
				}
				current, err := e.App.Series.Get(e.Ctx, ser.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err := sourcepriority.Apply(e.Ctx, e.App.DB, *current, links); err != nil {
					t.Fatal(err)
				}
				got := ""
				for _, link := range links {
					got += link.SourceID
				}
				if got != want {
					t.Fatalf("source order %q, want %q", got, want)
				}
			}

			assertOrder("CBA")
			custom := "custom"
			if _, err := e.App.Series.Update(e.Ctx, ser.ID, series.UpdateRequest{SourcePriorityMode: &custom}); err != nil {
				t.Fatal(err)
			}
			lists[0].Sources = []string{sourcepriority.Key(mod, "A")}
			if _, err := e.App.DB.NewUpdate().Model(&lists[0]).WherePK().Exec(e.Ctx); err != nil {
				t.Fatal(err)
			}
			assertOrder("CBA")

			inherit := "inherit"
			if _, err := e.App.Series.Update(e.Ctx, ser.ID, series.UpdateRequest{SourcePriorityMode: &inherit}); err != nil {
				t.Fatal(err)
			}
			assertOrder("ABC")
		})
	}
}
