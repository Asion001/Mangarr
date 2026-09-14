package sourcegov

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/model"
)

// fakeClock advances only when the governor sleeps.
type fakeClock struct {
	mu     sync.Mutex
	t      time.Time
	sleeps []time.Duration
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) sleep(_ context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d > 0 {
		c.sleeps = append(c.sleeps, d)
		c.t = c.t.Add(d)
	}
	return nil
}

func newTestGov(cfg model.ThrottleConfig, events *[]CooldownEvent) (*Governor, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	g := New(func(Key) model.ThrottleConfig { return Resolve(cfg) }, func(e CooldownEvent) {
		if events != nil {
			*events = append(*events, e)
		}
	})
	g.now, g.sleep = clk.now, clk.sleep
	g.rnd = func(n int64) int64 { return n - 1 } // always the maximum
	return g, clk
}

var k = Key{ModuleID: 1, SourceID: "A"}

func TestResolve(t *testing.T) {
	got := Resolve(model.ThrottleConfig{Preset: "normal", RequestsPerMinute: 60}, model.ThrottleConfig{MaxConcurrent: 2})
	if got.RequestsPerMinute != 60 || got.MaxConcurrent != 2 || got.ChapterGapMaxSec != Presets["normal"].ChapterGapMaxSec {
		t.Fatalf("layering: %+v", got)
	}
	// a catalog that picks another preset ignores the global fields for the old preset
	got = Resolve(model.ThrottleConfig{Preset: "normal", RequestsPerMinute: 60}, model.ThrottleConfig{Preset: "gentle"})
	if got != Presets["gentle"] {
		t.Fatalf("preset switch: %+v", got)
	}
	if got := Resolve(); got != Presets["normal"] {
		t.Fatalf("default: %+v", got)
	}
	if Resolve(model.ThrottleConfig{Preset: "fast"}).ChapterGapMaxSec != 0 {
		t.Fatal("fast must not pause between chapters")
	}
}

func TestClassify(t *testing.T) {
	for msg, want := range map[string]bool{
		"graphql: HTTP error 429": true,
		"Too Many Requests":       true,
		"java.io.IOException: Cloudflare bypass currently disabled": true,
		"HTTP error 403":     true,
		"HTTP error 503":     true,
		"HTTP error 404":     false,
		"connection refused": false,
	} {
		if _, got := Classify(errors.New(msg)); got != want {
			t.Errorf("Classify(%q) = %v", msg, got)
		}
	}
	if _, got := Classify(&ErrCoolingDown{}); got {
		t.Error("a cooldown error must not extend the cooldown")
	}
}

func TestTokenBucketAndDelays(t *testing.T) {
	g, clk := newTestGov(model.ThrottleConfig{Preset: "fast", RequestsPerMinute: 60, Burst: 2, MinDelayMs: 100, JitterMs: 50}, nil)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		rel, err := g.Acquire(ctx, k)
		if err != nil {
			t.Fatal(err)
		}
		rel()
	}
	// 1st: free; 2nd: min gap 150ms (100 + max jitter 50); 3rd: bucket empty -> ~0.85s; 4th: 1s
	var total time.Duration
	for _, s := range clk.sleeps {
		total += s
	}
	if len(clk.sleeps) < 2 || total < 1900*time.Millisecond || total > 2200*time.Millisecond {
		t.Fatalf("unexpected waits %v (total %v)", clk.sleeps, total)
	}
}

func TestConcurrencyCap(t *testing.T) {
	g := New(func(Key) model.ThrottleConfig { return Resolve(model.ThrottleConfig{Preset: "fast", MaxConcurrent: 1}) }, nil)
	ctx := context.Background()
	rel1, _ := g.Acquire(ctx, k)
	got := make(chan struct{})
	go func() {
		rel2, err := g.Acquire(ctx, k)
		if err == nil {
			rel2()
		}
		close(got)
	}()
	select {
	case <-got:
		t.Fatal("second request must wait")
	case <-time.After(50 * time.Millisecond):
	}
	rel1()
	select {
	case <-got:
	case <-time.After(time.Second):
		t.Fatal("second request not released")
	}
	// cancelled waiters give up
	rel1, _ = g.Acquire(ctx, k)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := g.Acquire(cctx, k); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	rel1()
}

func TestPace(t *testing.T) {
	g, clk := newTestGov(model.ThrottleConfig{Preset: "fast", ChapterGapMinSec: 2, ChapterGapMaxSec: 6}, nil)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := g.Pace(ctx, k, "chapter"); err != nil {
			t.Fatal(err)
		}
	}
	// first chapter immediately, then the maximum gap (rnd returns max) each time
	if len(clk.sleeps) != 2 || clk.sleeps[0] != 6*time.Second || clk.sleeps[1] != 6*time.Second {
		t.Fatalf("pace sleeps %v", clk.sleeps)
	}
	// refresh pacing is independent and disabled for "fast"
	if err := g.Pace(ctx, k, "refresh"); err != nil || len(clk.sleeps) != 2 {
		t.Fatalf("refresh pace: %v %v", err, clk.sleeps)
	}
}

func TestCooldown(t *testing.T) {
	var events []CooldownEvent
	g, clk := newTestGov(model.ThrottleConfig{Preset: "fast"}, &events)
	ctx := context.Background()
	err := g.Do(ctx, k, func(context.Context) error { return errors.New("HTTP error 429") })
	if err == nil || len(events) != 1 || events[0].Until == nil || events[0].Strikes != 1 {
		t.Fatalf("first strike: %v %+v", err, events)
	}
	if _, err := g.Acquire(ctx, k); err == nil {
		t.Fatal("expected cooldown error")
	} else if cd, ok := CoolingDown(err); !ok || !cd.Until.Equal(clk.now().Add(5*time.Minute)) {
		t.Fatalf("cooldown: %v", err)
	}
	// after the cooldown a second strike doubles it
	clk.t = clk.t.Add(6 * time.Minute)
	_ = g.Do(ctx, k, func(context.Context) error { return errors.New("HTTP error 429") })
	if until, _ := g.Cooldown(k); !until.Equal(clk.now().Add(10 * time.Minute)) {
		t.Fatalf("second strike until %v", until)
	}
	// success after the cooldown resets strikes and reports it
	clk.t = clk.t.Add(11 * time.Minute)
	if err := g.Do(ctx, k, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if last := events[len(events)-1]; last.Until != nil || last.Strikes != 0 {
		t.Fatalf("reset event: %+v", last)
	}
	if cooldownFor(10) != 2*time.Hour {
		t.Fatal("cooldown is capped at 2h")
	}
}
