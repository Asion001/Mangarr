package api_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/api"
	"github.com/Asion001/mangarr/internal/model"
)

func TestUpdatesListsNewWorksAndChapters(t *testing.T) {
	server, app := newServer(t, true)
	now := time.Now().UTC()
	root := &model.RootFolder{Path: t.TempDir(), Language: "ru", CreatedAt: now}
	if _, err := app.DB.NewInsert().Model(root).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	profile := &model.Profile{Name: "Updates test", IsDefault: false, CreatedAt: now, UpdatedAt: now}
	if _, err := app.DB.NewInsert().Model(profile).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	work := &model.Work{Title: "Синяя тюрьма", SortTitle: "синяя тюрьма", CreatedAt: now.Add(-time.Minute), UpdatedAt: now}
	if _, err := app.DB.NewInsert().Model(work).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	series := &model.Series{WorkID: work.ID, Title: work.Title, SortTitle: work.SortTitle, Status: model.StatusOngoing, Monitored: true,
		MonitorNew: model.MonitorAll, RootFolderID: root.ID, Path: "blue-lock", ProfileID: profile.ID, Language: "ru",
		ReadingDirection: "rtl", Tags: []int64{}, AddedAt: now.Add(-time.Minute), UpdatedAt: now}
	if _, err := app.DB.NewInsert().Model(series).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	chapter := &model.Chapter{SeriesID: series.ID, NumberKey: "1", NumberSort: 1, Title: "Мечта", Monitored: true,
		State: model.ChapterMissing, FirstSeenAt: now, UpdatedAt: now}
	if _, err := app.DB.NewInsert().Model(chapter).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(server.URL + "/api/v1/updates?days=7")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var items []api.UpdateItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(items) != 2 {
		t.Fatalf("status=%d updates=%+v", resp.StatusCode, items)
	}
	if items[0].Kind != "chapter" || items[0].ChapterID != chapter.ID || items[0].Language != "ru" {
		t.Fatalf("chapter update: %+v", items[0])
	}
	if items[1].Kind != "series" || items[1].SeriesTitle != work.Title || len(items[1].Languages) != 1 {
		t.Fatalf("series update: %+v", items[1])
	}
}
