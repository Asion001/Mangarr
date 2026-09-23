package api_test

import (
	"encoding/json"
	"io"
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
		State: model.ChapterMissing, FirstSeenAt: now.Add(10 * time.Minute), UpdatedAt: now}
	if _, err := app.DB.NewInsert().Model(chapter).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(server.URL + "/api/v1/updates?days=7")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var page api.UpdatePage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Total != 2 {
		t.Fatalf("status=%d updates=%+v", resp.StatusCode, page)
	}
	if page.Items[0].Kind != "chapter" || page.Items[0].ChapterID != chapter.ID || page.Items[0].Language != "ru" {
		t.Fatalf("chapter update: %+v", page.Items[0])
	}
	if page.Items[1].Kind != "series" || page.Items[1].SeriesTitle != work.Title || len(page.Items[1].Languages) != 1 {
		t.Fatalf("series update: %+v", page.Items[1])
	}
}

func TestUpdatesSuppressesInitialCatalogAndPaginates(t *testing.T) {
	server, app := newServer(t, true)
	now := time.Now().UTC()
	root := &model.RootFolder{Path: t.TempDir(), CreatedAt: now}
	_, _ = app.DB.NewInsert().Model(root).Exec(t.Context())
	profile := &model.Profile{Name: "Updates pagination", CreatedAt: now, UpdatedAt: now}
	_, _ = app.DB.NewInsert().Model(profile).Exec(t.Context())
	series := &model.Series{Title: "Feed", SortTitle: "feed", Status: model.StatusOngoing, Monitored: true, MonitorNew: model.MonitorAll,
		RootFolderID: root.ID, Path: "feed", ProfileID: profile.ID, Tags: []int64{}, AddedAt: now, UpdatedAt: now}
	_, _ = app.DB.NewInsert().Model(series).Exec(t.Context())
	initial := &model.Chapter{SeriesID: series.ID, NumberKey: "1", NumberSort: 1, State: model.ChapterMissing, FirstSeenAt: now.Add(time.Minute), UpdatedAt: now}
	newer := &model.Chapter{SeriesID: series.ID, NumberKey: "2", NumberSort: 2, State: model.ChapterMissing, FirstSeenAt: now.Add(10 * time.Minute), UpdatedAt: now}
	_, _ = app.DB.NewInsert().Model(initial).Exec(t.Context())
	_, _ = app.DB.NewInsert().Model(newer).Exec(t.Context())

	resp, err := http.Get(server.URL + "/api/v1/updates?days=7&kind=chapter&pageSize=1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var page api.UpdatePage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ChapterID != newer.ID {
		t.Fatalf("initial catalog was not suppressed: %+v", page)
	}
	bad, err := http.Get(server.URL + "/api/v1/updates?cursor=not-a-cursor")
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid cursor status=%d want 400", bad.StatusCode)
	}
}
