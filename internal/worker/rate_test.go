package worker

import (
	"context"
	"testing"
	"time"
)

// TestRateFromSpec: a worker keeps to the share of the catalog's budget the
// server gave it, and asks as fast as it likes when there is none.
func TestRateFromSpec(t *testing.T) {
	r := rateFrom(map[string]any{"rate": map[string]any{
		"requestsPerMinute": float64(120), "maxConcurrent": float64(2), "minDelayMs": float64(0), "jitterMs": float64(0),
	}})
	if r.perMinute != 120 || r.maxConcurrent != 2 {
		t.Fatalf("budget %+v", r)
	}
	// 120 a minute is one every 500ms: three requests take at least a second
	start := time.Now()
	for range 3 {
		if err := r.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d < time.Second {
		t.Fatalf("three requests took %s, which is faster than the budget allows", d)
	}

	free := rateFrom(map[string]any{})
	start = time.Now()
	for range 5 {
		if err := free.wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if d := time.Since(start); d > 100*time.Millisecond {
		t.Fatalf("a task with no budget waited %s", d)
	}
}

// TestRateStopsWithTheContext: a cancelled chapter doesn't leave fetchers
// sleeping on a limiter.
func TestRateStopsWithTheContext(t *testing.T) {
	r := rateFrom(map[string]any{"rate": map[string]any{"requestsPerMinute": float64(1)}})
	ctx, cancel := context.WithCancel(context.Background())
	if err := r.wait(ctx); err != nil { // the first is due at once
		t.Fatal(err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	if err := r.wait(ctx); err == nil {
		t.Fatal("waiting should have been cut short")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("it waited %s after being cancelled", d)
	}
}
