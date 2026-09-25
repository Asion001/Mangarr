package mediaserver

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/model"
)

const CacheTTL = 10 * time.Minute
const failureTTL = 30 * time.Second
const maxEntries = 1024

type entry[T any] struct {
	value T
	err   error
	until time.Time
}

// Cache coalesces concurrent loads and bounds both positive and negative results.
// Each configured instance owns its caches, so settings reloads invalidate them.
type Cache[T any] struct {
	mu      sync.Mutex
	entries map[string]entry[T]
	pending map[string]chan struct{}
}

func AdaptationKey(a model.Adaptation) string {
	b, _ := json.Marshal(struct {
		Title, Format string
		Year          int
		IDs           map[string]string
	}{a.Title, a.Format, a.Year, a.ExternalIDs})
	return string(b)
}

func (c *Cache[T]) Get(ctx context.Context, key string, load func(context.Context) (T, error)) (T, error) {
	for {
		if err := ctx.Err(); err != nil {
			var zero T
			return zero, err
		}
		c.mu.Lock()
		if e, ok := c.entries[key]; ok && time.Now().Before(e.until) {
			c.mu.Unlock()
			return e.value, e.err
		}
		if done := c.pending[key]; done != nil {
			c.mu.Unlock()
			select {
			case <-done:
				continue
			case <-ctx.Done():
				var zero T
				return zero, ctx.Err()
			}
		}
		if c.pending == nil {
			c.pending = map[string]chan struct{}{}
		}
		done := make(chan struct{})
		c.pending[key] = done
		c.mu.Unlock()
		value, err := load(ctx)
		ttl := CacheTTL
		if err != nil {
			ttl = failureTTL
		}
		c.mu.Lock()
		if c.entries == nil || len(c.entries) >= maxEntries {
			c.entries = map[string]entry[T]{}
		}
		if ctx.Err() != context.Canceled {
			c.entries[key] = entry[T]{value, err, time.Now().Add(ttl)}
		}
		delete(c.pending, key)
		close(done)
		c.mu.Unlock()
		return value, err
	}
}
