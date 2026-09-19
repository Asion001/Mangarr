package app_test

import (
	"errors"
	"testing"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/metadataagg"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

func TestLanguageEditionsShareWork(t *testing.T) {
	for dialect, dsn := range dbtest.DSNs(t) {
		t.Run(dialect, func(t *testing.T) {
			scenario := fakesource.NewScenario("editions")
			scenario.Sources = []source.SourceInfo{{ID: "en", Name: "English", Lang: "en"}, {ID: "ru", Name: "Russian", Lang: "ru"}}
			scenario.AddManga(&fakesource.Manga{SourceID: "en", URL: "/blue-lock", Title: "Blue Lock"})
			scenario.AddManga(&fakesource.Manga{SourceID: "ru", URL: "/blue-lock", Title: "Синяя тюрьма"})
			e := newTestApp(t, dsn)
			sourceModule := e.addFakeModule(t, "editions")
			meta := &model.ProviderDefinition{Kind: "metadata", Implementation: "fakemeta", Name: "Metadata", Enabled: true}
			if err := e.App.Modules.Create(e.Ctx, meta); err != nil {
				t.Fatal(err)
			}
			add := func(lang, sourceID, title string) (*model.Series, error) {
				return e.App.Series.Add(e.Ctx, series.AddRequest{
					Metadata:     &metadataagg.Ref{ModuleID: meta.ID, Provider: "fakemeta", ID: "42"},
					Sources:      []series.SourceLink{{ModuleID: sourceModule, SourceID: sourceID, URL: "/blue-lock", Title: title, Lang: lang}},
					RootFolderID: e.RFID, Language: lang, Monitor: model.MonitorNone, NoRefresh: true,
				})
			}
			en, err := add("en", "en", "Blue Lock")
			if err != nil {
				t.Fatal(err)
			}
			ru, err := add("ru", "ru", "Синяя тюрьма")
			if err != nil {
				t.Fatal(err)
			}
			if en.WorkID == 0 || ru.WorkID != en.WorkID {
				t.Fatalf("editions were not grouped: en=%+v ru=%+v", en, ru)
			}
			if _, err := add("en", "en", "Blue Lock"); !errors.Is(err, series.ErrExists) {
				t.Fatalf("same-language duplicate was accepted: %v", err)
			}
			if err := e.App.Series.SetWork(e.Ctx, ru.ID, 0); err != nil {
				t.Fatal(err)
			}
			ru, _ = e.App.Series.Get(e.Ctx, ru.ID)
			if ru.WorkID == en.WorkID {
				t.Fatal("separating an edition kept it in the original work")
			}
		})
	}
}
