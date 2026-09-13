package jobs_test

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/jobs"
	"github.com/Asion001/mangarr/internal/model"
)

func TestQueueDedupAndExclusive(t *testing.T) {
	d := dbtest.SQLite(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := jobs.NewQueue(d, events.NewBus(), log, 3)

	release := make(chan struct{})
	var running, maxRunning atomic.Int32
	track := func(ctx context.Context, r *jobs.Run) error {
		n := running.Add(1)
		for {
			m := maxRunning.Load()
			if n <= m || maxRunning.CompareAndSwap(m, n) {
				break
			}
		}
		<-release
		running.Add(-1)
		return nil
	}
	q.Register(jobs.Definition{Name: "Slow", Handler: track})
	q.Register(jobs.Definition{Name: "Other", Handler: track})
	var exclusiveSawOthers atomic.Bool
	q.Register(jobs.Definition{Name: "Exclusive", Exclusive: true, Handler: func(ctx context.Context, r *jobs.Run) error {
		if running.Load() != 0 {
			exclusiveSawOthers.Store(true)
		}
		return nil
	}})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	a, _ := q.Push(ctx, "Slow", map[string]any{"seriesId": 1}, "manual")
	b, _ := q.Push(ctx, "Slow", map[string]any{"seriesId": 1}, "manual")
	if a.ID != b.ID {
		t.Fatalf("expected dedup, got %d and %d", a.ID, b.ID)
	}
	o, _ := q.Push(ctx, "Other", nil, "manual")
	ex, _ := q.Push(ctx, "Exclusive", nil, "manual")

	time.Sleep(200 * time.Millisecond)
	close(release)

	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	defer wcancel()
	for _, id := range []int64{a.ID, o.ID, ex.ID} {
		c, err := q.Wait(wctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if c.Status != model.CommandCompleted {
			t.Fatalf("command %s status %s err %s", c.Name, c.Status, c.Error)
		}
	}
	if exclusiveSawOthers.Load() {
		t.Fatal("exclusive command ran concurrently with others")
	}
	if maxRunning.Load() != 2 {
		t.Fatalf("expected Slow and Other to run in parallel, max=%d", maxRunning.Load())
	}
}

func TestQueueFailureRecorded(t *testing.T) {
	d := dbtest.SQLite(t)
	q := jobs.NewQueue(d, events.NewBus(), slog.New(slog.NewTextHandler(io.Discard, nil)), 1)
	q.Register(jobs.Definition{Name: "Boom", Handler: func(ctx context.Context, r *jobs.Run) error { panic("kaboom") }})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = q.Start(ctx)
	c, _ := q.Push(ctx, "Boom", nil, "manual")
	wctx, wcancel := context.WithTimeout(ctx, 5*time.Second)
	defer wcancel()
	got, err := q.Wait(wctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.CommandFailed || got.Error == "" {
		t.Fatalf("expected failure, got %+v", got)
	}
}
