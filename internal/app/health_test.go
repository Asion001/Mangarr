package app_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/uptrace/bun"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules/source"
	"github.com/Asion001/mangarr/internal/series"
	"github.com/Asion001/mangarr/internal/testutil/fakesource"
)

// TestHealthFailingSourceLinksSeries: the failing-source warning lists the
// affected series with links (titles can contain commas).
func TestHealthFailingSourceLinksSeries(t *testing.T) {
	sc := fakesource.NewScenario("health")
	sc.Sources = []source.SourceInfo{{ID: "A", Name: "MangaLib", Lang: "ru"}}
	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	mod := e.addFakeModule(t, "health")
	var ids []int64
	for _, title := range []string{"Please Put Them On, Takamine-san", "The Virgin Witch"} {
		ser, err := e.App.Series.Add(e.Ctx, series.AddRequest{Title: title, RootFolderID: e.RFID, Monitor: model.MonitorNone,
			Sources: []series.SourceLink{{ModuleID: mod, SourceID: "A", URL: "/" + title, SourceName: "MangaLib (RU)", Lang: "ru"}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, ser.ID)
	}
	// let the refreshes queued by adding them finish (they fail and count too)
	waitFor(t, 20*time.Second, "refreshes", func() bool {
		n, _ := e.App.DB.NewSelect().Model((*model.Command)(nil)).Where("status IN (?)", bun.In([]string{model.CommandQueued, model.CommandStarted})).Count(e.Ctx)
		return n == 0
	})
	_, _ = e.App.DB.NewUpdate().Model((*model.SeriesSource)(nil)).Set("consecutive_failures = 4").Set("last_error = 'HTTP 403'").Where("1 = 1").Exec(e.Ctx)
	var found bool
	for _, c := range e.App.Health.Run(e.Ctx) {
		if c.Source != "Sources" {
			continue
		}
		found = true
		if c.Message != "MangaLib (RU) keeps failing for 2 series" || len(c.Items) != 2 {
			t.Fatalf("check %+v", c)
		}
		links := map[string]bool{c.Items[0].Link: true, c.Items[1].Link: true}
		for _, id := range ids {
			if !links[fmt.Sprintf("/series/%d", id)] {
				t.Fatalf("items %+v", c.Items)
			}
		}
		if c.Items[0].Detail != "4 failed checks in a row: HTTP 403" {
			t.Fatalf("detail %q", c.Items[0].Detail)
		}
		if txt := c.Text(); txt == c.Message {
			t.Fatalf("notification text lacks the titles: %q", txt)
		}
	}
	if !found {
		t.Fatal("no failing-source warning")
	}
}
