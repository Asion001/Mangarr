package api_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/api"
	"github.com/Asion001/mangarr/internal/cbz"
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

func TestPreviewNeedsAnUpscaler(t *testing.T) {
	srv, a := newServer(t, true)
	ctx := context.Background()
	now := time.Now().UTC()
	root := &model.RootFolder{Path: t.TempDir(), CreatedAt: now}
	if _, err := a.DB.NewInsert().Model(root).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	ser := &model.Series{Title: "Preview", SortTitle: "preview", RootFolderID: root.ID, ProfileID: 1, Path: "Preview", Tags: []int64{}, AddedAt: now, UpdatedAt: now}
	if _, err := a.DB.NewInsert().Model(ser).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	var pages []cbz.Page
	for i := range 3 {
		var buf bytes.Buffer
		_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, 400, 600)))
		pages = append(pages, cbz.Page{Name: fmt.Sprintf("%04d.png", i+1), Data: buf.Bytes()})
	}
	if err := os.MkdirAll(filepath.Join(root.Path, "Preview"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := cbz.Write(filepath.Join(root.Path, "Preview", "1.cbz"), pages, nil, 0o644, now); err != nil {
		t.Fatal(err)
	}
	ch := &model.Chapter{SeriesID: ser.ID, NumberKey: "1", NumberSort: 1, FirstSeenAt: now, UpdatedAt: now}
	if _, err := a.DB.NewInsert().Model(ch).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	f := &model.ChapterFile{ChapterID: ch.ID, SeriesID: ser.ID, RelativePath: "1.cbz", ImportedAt: now}
	if _, err := a.DB.NewInsert().Model(f).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	c := caller{t, http.DefaultClient, srv.URL}
	body := func(upscale bool, format string) string {
		return fmt.Sprintf(`{"chapterId":%d,"encode":{"format":%q,"preset":"balanced","quality":0,"speed":0,"grayscale":true,"progressive":false,"minSavingsPct":10,"recycleOriginals":false},`+
			`"upscale":{"enabled":%t,"upscalerId":0,"minWidth":1400,"maxWidth":0,"model":"waifu2x-cunet","noise":1,"format":"source","quality":90}}`, ch.ID, format, upscale)
	}
	if code := c.do("POST", "/api/v1/processing/preview", body(false, "keep"), nil); code != 422 {
		t.Fatalf("nothing to preview: %d", code)
	}
	// no upscaler module is configured in tests: the UI shows its own message for 409
	resp, err := http.Post(srv.URL+"/api/v1/processing/preview", "application/json", strings.NewReader(body(true, "keep")))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 409 || !strings.Contains(string(msg), "no upscaler available") {
		t.Fatalf("no upscaler: %d %s", resp.StatusCode, msg)
	}
}
