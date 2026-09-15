package reading

import (
	"sync"
	"time"
)

// countCache remembers page counts of streamed chapters for a while, so
// book lists can show them without asking the source again.
type countCache struct {
	mu sync.Mutex
	m  map[int64]countEntry
}

type countEntry struct {
	n   int
	exp time.Time
}

const pageListTTL = 30 * time.Minute

func (c *countCache) get(id int64) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[id]
	if !ok || time.Now().After(e.exp) {
		return 0
	}
	return e.n
}

func (c *countCache) set(id int64, n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[int64]countEntry{}
	}
	now := time.Now()
	if len(c.m) > 4096 { // drop expired entries now and then
		for k, e := range c.m {
			if now.After(e.exp) {
				delete(c.m, k)
			}
		}
	}
	c.m[id] = countEntry{n: n, exp: now.Add(pageListTTL)}
}
