package api_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/api"
	"github.com/Asion001/mangarr/internal/app"
	"github.com/Asion001/mangarr/internal/config"
	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/logging"
	"github.com/Asion001/mangarr/internal/model"
)

func TestQueueRankAPI(t *testing.T) {
	for dialect, dsn := range dbtest.DSNs(t) {
		t.Run(dialect, func(t *testing.T) {
			ctx := t.Context()
			_, ring := logging.Setup("error", io.Discard)
			a, err := app.New(ctx, &config.Config{DataDir: t.TempDir(), DB: dsn, AuthDisabled: true}, slog.New(slog.NewTextHandler(io.Discard, nil)), ring)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = a.Close() })
			srv := httptest.NewServer(api.New(a))
			defer srv.Close()
			c := caller{t, http.DefaultClient, srv.URL}
			now := time.Now().UTC()
			root := &model.RootFolder{Path: t.TempDir(), CreatedAt: now}
			if _, err := a.DB.NewInsert().Model(root).Exec(ctx); err != nil {
				t.Fatal(err)
			}
			ser := &model.Series{Title: "Copper Clouds", SortTitle: "copper clouds", RootFolderID: root.ID, ProfileID: 1, Path: "copper", Tags: []int64{}, AddedAt: now, UpdatedAt: now}
			if _, err := a.DB.NewInsert().Model(ser).Exec(ctx); err != nil {
				t.Fatal(err)
			}
			var jobs []*model.DownloadJob
			for i := 0; i < 6; i++ {
				ch := &model.Chapter{SeriesID: ser.ID, NumberKey: fmt.Sprint(i + 1), NumberSort: float64(i + 1), State: model.ChapterMissing, FirstSeenAt: now, UpdatedAt: now}
				if _, err := a.DB.NewInsert().Model(ch).Exec(ctx); err != nil {
					t.Fatal(err)
				}
				kind := model.JobKindDownload
				if i%2 == 1 {
					kind = model.JobKindReprocess
				}
				job, _, err := a.DLQueue.Enqueue(ctx, ser.ID, ch.ID, nil, kind, false)
				if err != nil {
					t.Fatal(err)
				}
				jobs = append(jobs, job)
			}
			var affected struct {
				Affected int `json:"affected"`
			}
			for _, action := range []string{"top", "bottom"} {
				body := fmt.Sprintf(`{"ids":[%d,%d],"action":%q}`, jobs[4].ID, jobs[2].ID, action)
				if code := c.do("POST", "/api/v1/queue/bulk", body, &affected); code != 200 || affected.Affected != 2 {
					t.Fatalf("legacy %s: %d %+v", action, code, affected)
				}
			}
			body := fmt.Sprintf(`{"ids":[%d,%d],"action":"before","anchorId":%d}`, jobs[4].ID, jobs[2].ID, jobs[0].ID)
			if code := c.do("POST", "/api/v1/queue/bulk", body, &affected); code != 200 || affected.Affected != 2 {
				t.Fatalf("before: %d %+v", code, affected)
			}
			var page api.QueueResponse
			if code := c.do("GET", "/api/v1/queue?pageSize=1&page=2", "", &page); code != 200 {
				t.Fatal(code)
			}
			if page.Total != 6 || len(page.Items) != 1 || page.Items[0].ID != jobs[4].ID {
				t.Fatalf("pagination: %+v", page)
			}
			rank := page.Items[0].Rank
			if rank == jobs[4].Rank {
				t.Fatal("move did not update rank")
			}
			// A newer finished attempt must not hide an older active job's rank.
			finished := *jobs[4]
			finished.ID, finished.Status = 0, model.JobCompleted
			if _, err := a.DB.NewInsert().Model(&finished).Exec(ctx); err != nil {
				t.Fatal(err)
			}
			var chapters []api.ChapterResource
			if code := c.do("GET", fmt.Sprintf("/api/v1/series/%d/chapters", ser.ID), "", &chapters); code != 200 {
				t.Fatal(code)
			}
			found := false
			for _, ch := range chapters {
				if ch.ID == jobs[4].ChapterID {
					found = true
					if ch.Job == nil || ch.Job.ID != jobs[4].ID || ch.Job.Rank != rank {
						t.Fatalf("chapter rank differs from queue: %+v", ch.Job)
					}
				}
			}
			if !found {
				t.Fatal("chapter absent")
			}
			body = fmt.Sprintf(`{"filter":{"kind":"reprocess"},"action":"after","anchorId":%d}`, jobs[0].ID)
			if code := c.do("POST", "/api/v1/queue/bulk", body, &affected); code != 200 || affected.Affected != 3 {
				t.Fatalf("filter move: %d %+v", code, affected)
			}
			if code := c.do("GET", fmt.Sprintf("/api/v1/queue?page=2&pageSize=1&revision=%d", page.Revision), "", nil); code != 409 {
				t.Fatalf("stale revision accepted: %d", code)
			}
			if code := c.do("GET", "/api/v1/queue?pageSize=2", "", &page); code != 200 {
				t.Fatal(code)
			}
			ids := []int64{}
			for i := 1; i <= 3; i++ {
				if code := c.do("GET", fmt.Sprintf("/api/v1/queue?page=%d&pageSize=2&revision=%d", i, page.Revision), "", &page); code != 200 {
					t.Fatal(code)
				}
				for _, job := range page.Items {
					ids = append(ids, job.ID)
				}
			}
			want := []int64{jobs[2].ID, jobs[4].ID, jobs[0].ID, jobs[1].ID, jobs[3].ID, jobs[5].ID}
			if !slices.Equal(ids, want) {
				t.Fatalf("filter move order: %v want %v", ids, want)
			}
			body = fmt.Sprintf(`{"ids":[%d],"action":"after","anchorId":%d}`, jobs[0].ID, jobs[0].ID)
			if code := c.do("POST", "/api/v1/queue/bulk", body, nil); code != 400 {
				t.Fatalf("self anchor accepted: %d", code)
			}
			body = fmt.Sprintf(`{"ids":[%d],"action":"before"}`, jobs[0].ID)
			if code := c.do("POST", "/api/v1/queue/bulk", body, nil); code != 400 {
				t.Fatalf("missing anchor accepted: %d", code)
			}
		})
	}
}
