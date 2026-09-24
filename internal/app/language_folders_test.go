package app_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/metadataagg"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

func TestAddEditionsSplitsByLanguage(t *testing.T) {
	for dialect, dsn := range dbtest.DSNs(t) {
		t.Run(dialect, func(t *testing.T) {
			scenario := fakesource.NewScenario("langfolders")
			scenario.Sources = []source.SourceInfo{
				{ID: "en1", Name: "English 1", Lang: "en"}, {ID: "en2", Name: "English 2", Lang: "en"},
				{ID: "ru", Name: "Russian", Lang: "ru"}, {ID: "ja", Name: "Japanese", Lang: "ja"},
				{ID: "ko", Name: "Korean", Lang: "ko"}, {ID: "all", Name: "Everything", Lang: "all"},
			}
			for _, id := range []string{"en1", "en2", "ru", "ja", "ko", "all"} {
				scenario.AddManga(&fakesource.Manga{SourceID: id, URL: "/bl", Title: "Blue Lock"})
			}
			e := newTestApp(t, dsn)
			mod := e.addFakeModule(t, "langfolders")
			meta := &model.ProviderDefinition{Kind: "metadata", Implementation: "fakemeta", Name: "Metadata", Enabled: true}
			if err := e.App.Modules.Create(e.Ctx, meta); err != nil {
				t.Fatal(err)
			}
			lib := filepath.Join(t.TempDir(), "library")
			// the automatic folder: every language without its own gets <lib>/<lang>
			auto := &model.RootFolder{Path: lib, Language: "*", CreatedAt: time.Now().UTC()}
			if _, err := e.App.DB.NewInsert().Model(auto).Exec(e.Ctx); err != nil {
				t.Fatal(err)
			}
			link := func(id, lang string) series.SourceLink {
				return series.SourceLink{ModuleID: mod, SourceID: id, URL: "/bl", Title: "Blue Lock", Lang: lang}
			}

			// EN primary + fallback and RU in one add: two editions of one title
			res, err := e.App.Series.AddEditions(e.Ctx, series.AddEditionsRequest{
				Metadata: &metadataagg.Ref{ModuleID: meta.ID, Provider: "fakemeta", ID: "42"},
				Sources:  []series.SourceLink{link("en1", "en"), link("ru", "ru"), link("en2", "en")},
				Monitor:  model.MonitorNone, NoRefresh: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Editions) != 2 {
				t.Fatalf("editions = %d, want 2", len(res.Editions))
			}
			en, ru := res.Editions[0], res.Editions[1]
			if en.Language != "en" || ru.Language != "ru" || en.WorkID != ru.WorkID || en.WorkID != res.WorkID {
				t.Fatalf("editions not split into one work: en=%+v ru=%+v", en, ru)
			}
			if en.RootFolderID != e.RFID {
				t.Fatalf("en edition went to folder %d, want the existing en folder %d", en.RootFolderID, e.RFID)
			}
			ruFolder, err := e.App.Library.RootFolder(e.Ctx, ru.RootFolderID)
			if err != nil || ruFolder.Path != filepath.Join(lib, "ru") || ruFolder.Language != "ru" {
				t.Fatalf("ru folder = %+v, %v", ruFolder, err)
			}
			var enLinks []model.SeriesSource
			if err := e.App.DB.NewSelect().Model(&enLinks).Where("series_id = ?", en.ID).Order("priority").Scan(e.Ctx); err != nil {
				t.Fatal(err)
			}
			if len(enLinks) != 2 || enLinks[0].SourceID != "en1" || enLinks[1].SourceID != "en2" {
				t.Fatalf("en sources = %+v", enLinks)
			}

			// adding a language to the title: no metadata search, same work;
			// a multi-language catalog takes the language it was picked for
			res2, err := e.App.Series.AddEditions(e.Ctx, series.AddEditionsRequest{
				WorkID: res.WorkID, Sources: []series.SourceLink{link("ja", "ja"), link("all", "ja"), link("en1", "en")},
				Monitor: model.MonitorNone, NoRefresh: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(res2.Editions) != 2 || res2.Editions[0].Language != "ja" || res2.Editions[0].WorkID != res.WorkID || res2.Editions[1].ID != en.ID {
				t.Fatalf("add language = %+v", res2.Editions)
			}
			if n, _ := e.App.DB.NewSelect().Model((*model.SeriesSource)(nil)).Where("series_id = ?", en.ID).Count(e.Ctx); n != 2 {
				t.Fatalf("en sources after re-adding en1 = %d, want 2", n)
			}

			// a multi-language catalog without a language is refused
			if _, err := e.App.Series.AddEditions(e.Ctx, series.AddEditionsRequest{
				Sources: []series.SourceLink{link("all", "all")}, Monitor: model.MonitorNone, NoRefresh: true,
			}); !isValidation(err) {
				t.Fatalf("multi-language source without a language: %v", err)
			}

			// no automatic folder: a language without one blocks the whole add
			if _, err := e.App.DB.NewDelete().Model(auto).WherePK().Exec(e.Ctx); err != nil {
				t.Fatal(err)
			}
			before, _ := e.App.DB.NewSelect().Model((*model.Series)(nil)).Count(e.Ctx)
			_, err = e.App.Series.AddEditions(e.Ctx, series.AddEditionsRequest{
				Title: "Other", Sources: []series.SourceLink{link("en2", "en"), link("ko", "ko")}, Monitor: model.MonitorNone, NoRefresh: true,
			})
			if !isValidation(err) {
				t.Fatalf("missing ko folder: %v", err)
			}
			if after, _ := e.App.DB.NewSelect().Model((*model.Series)(nil)).Count(e.Ctx); after != before {
				t.Fatalf("a blocked add created %d series", after-before)
			}
		})
	}
}

func isValidation(err error) bool {
	var ve series.ValidationError
	return errors.As(err, &ve)
}

func TestAssignFolderLanguages(t *testing.T) {
	for dialect, dsn := range dbtest.DSNs(t) {
		t.Run(dialect, func(t *testing.T) {
			e := newTestApp(t, dsn)
			rf := &model.RootFolder{Path: filepath.Join(t.TempDir(), "ru"), CreatedAt: time.Now().UTC()}
			if _, err := e.App.DB.NewInsert().Model(rf).Exec(e.Ctx); err != nil {
				t.Fatal(err)
			}
			for i, lang := range []string{"ru", "ru", "en"} {
				ser := &model.Series{Title: "S", SortTitle: "s", Status: model.StatusUnknown, MonitorNew: "all", RootFolderID: rf.ID,
					Path: string(rune('a' + i)), ProfileID: 1, Language: lang, ReadingDirection: "rtl", Tags: []int64{}, AddedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
				if _, err := e.App.DB.NewInsert().Model(ser).Exec(e.Ctx); err != nil {
					t.Fatal(err)
				}
			}
			if err := e.App.Library.AssignFolderLanguages(e.Ctx); err != nil {
				t.Fatal(err)
			}
			got, _ := e.App.Library.RootFolder(e.Ctx, rf.ID)
			if got.Language != "ru" {
				t.Fatalf("language = %q, want ru (most of its series)", got.Language)
			}
		})
	}
}
