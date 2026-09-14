package sourcecache

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLRUAndTTL(t *testing.T) {
	c := New(1000)
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }
	c.Set("a", 1, 400, time.Minute)
	c.Set("b", 2, 400, time.Minute)
	if _, ok := c.Get("a"); !ok { // a becomes most recent
		t.Fatal("a missing")
	}
	c.Set("c", 3, 400, time.Minute) // evicts b (least recent)
	if _, ok := c.Get("b"); ok {
		t.Fatal("b should be evicted")
	}
	if n, b, _ := c.Stats(); n != 2 || b != 800 {
		t.Fatalf("stats %d %d", n, b)
	}
	now = now.Add(2 * time.Minute)
	if _, ok := c.Get("a"); ok {
		t.Fatal("a should be expired")
	}
	c.Set("huge", 1, 5000, time.Minute)
	if _, ok := c.Get("huge"); ok {
		t.Fatal("entries larger than the cache are not stored")
	}
	c.Set("x1", 1, 10, time.Minute)
	c.Set("y1", 1, 10, time.Minute)
	c.DeletePrefix("x")
	if _, ok := c.Get("x1"); ok {
		t.Fatal("prefix delete")
	}
}

func TestDoCoalescesAndSkipsErrors(t *testing.T) {
	c := New(1 << 20)
	var calls atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, _, err := Do(c, "k", time.Minute, func() (string, error) {
				calls.Add(1)
				time.Sleep(50 * time.Millisecond)
				return "v", nil
			})
			if err != nil || v != "v" {
				t.Errorf("got %q %v", v, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("want 1 upstream call, got %d", calls.Load())
	}
	if _, cached, _ := Do(c, "k", time.Minute, func() (string, error) { return "x", nil }); !cached {
		t.Fatal("second call should be cached")
	}
	_, _, err := Do(c, "e", time.Minute, func() (int, error) { return 0, errors.New("boom") })
	if err == nil {
		t.Fatal("expected error")
	}
	if v, _, _ := Do(c, "e", time.Minute, func() (int, error) { return 7, nil }); v != 7 {
		t.Fatal("errors must not be cached")
	}
}
