package api_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/api"
	"github.com/Asion001/mangarr/internal/model"
)

func TestProcessingGrowthAndUnknownOriginal(t *testing.T) {
	srv, a := newServer(t, true)
	ctx := context.Background()
	now := time.Now().UTC()
	root := &model.RootFolder{Path: t.TempDir(), CreatedAt: now}
	if _, err := a.DB.NewInsert().Model(root).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	ser := &model.Series{Title: "Processing", SortTitle: "processing", RootFolderID: root.ID, ProfileID: 1, Path: "Processing", Tags: []int64{}, AddedAt: now, UpdatedAt: now}
	if _, err := a.DB.NewInsert().Model(ser).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	for i, sizes := range [][2]int64{{1000, 600}, {1000, 1530}, {1000, 1000}, {0, 900}} {
		ch := &model.Chapter{SeriesID: ser.ID, NumberKey: fmt.Sprint(i), NumberSort: float64(i), FirstSeenAt: now, UpdatedAt: now}
		if _, err := a.DB.NewInsert().Model(ch).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		f := &model.ChapterFile{ChapterID: ch.ID, SeriesID: ser.ID, RelativePath: fmt.Sprintf("%d.cbz", i), SizeOriginal: sizes[0], Size: sizes[1], ProcessState: model.ProcessDone, ProcessSeconds: 1046, ProcessPages: 13, ProcessedAt: &now, ImportedAt: now}
		if _, err := a.DB.NewInsert().Model(f).Exec(ctx); err != nil {
			t.Fatal(err)
		}
	}
	c := caller{t, http.DefaultClient, srv.URL}
	var got api.ProcessingStatus
	if code := c.do("GET", "/api/v1/processing", "", &got); code != 200 {
		t.Fatalf("status: %d", code)
	}
	if got.Processed != 4 || got.SpaceSaved != 400 || got.SpaceAdded != 530 || got.NetSpaceSaved != -130 {
		t.Fatalf("totals: %+v", got)
	}
	if got.PagesPerMinute < 0.74 || got.PagesPerMinute > 0.75 {
		t.Fatalf("speed: %v", got.PagesPerMinute)
	}
	var days []api.ProcessingDay
	if code := c.do("GET", "/api/v1/processing/history?days=1", "", &days); code != 200 {
		t.Fatalf("history: %d", code)
	}
	if len(days) != 1 || days[0].BytesBefore-days[0].BytesAfter != -130 {
		t.Fatalf("history: %+v", days)
	}
}
