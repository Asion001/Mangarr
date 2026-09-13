// Package logging sets up slog with a stdout handler plus an in-memory ring
// buffer that the UI can read (System → Logs).
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Attrs   map[string]string `json:"attrs,omitempty"`
}

// Ring keeps the last N log entries.
type Ring struct {
	mu      sync.Mutex
	entries []Entry
	next    int
	full    bool
}

func NewRing(size int) *Ring { return &Ring{entries: make([]Entry, size)} }

func (r *Ring) add(e Entry) {
	r.mu.Lock()
	r.entries[r.next] = e
	r.next = (r.next + 1) % len(r.entries)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// Entries returns entries newest first, optionally filtered by minimum level.
func (r *Ring) Entries(minLevel slog.Level, limit int) []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.next
	if r.full {
		n = len(r.entries)
	}
	out := make([]Entry, 0, min(n, limit))
	for i := 0; i < n && len(out) < limit; i++ {
		idx := (r.next - 1 - i + len(r.entries)) % len(r.entries)
		e := r.entries[idx]
		if ParseLevel(e.Level) >= minLevel {
			out = append(out, e)
		}
	}
	return out
}

type ringHandler struct {
	ring  *Ring
	level slog.Leveler
	attrs []slog.Attr
	group string
}

func (h *ringHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level.Level() }

func (h *ringHandler) Handle(_ context.Context, rec slog.Record) error {
	e := Entry{Time: rec.Time, Level: rec.Level.String(), Message: rec.Message}
	add := func(a slog.Attr) {
		if e.Attrs == nil {
			e.Attrs = map[string]string{}
		}
		key := a.Key
		if h.group != "" {
			key = h.group + "." + key
		}
		e.Attrs[key] = a.Value.String()
	}
	for _, a := range h.attrs {
		add(a)
	}
	rec.Attrs(func(a slog.Attr) bool { add(a); return true })
	h.ring.add(e)
	return nil
}

func (h *ringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &c
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	c := *h
	if c.group != "" {
		name = c.group + "." + name
	}
	c.group = name
	return &c
}

type multiHandler []slog.Handler

func (m multiHandler) Enabled(ctx context.Context, l slog.Level) bool {
	for _, h := range m {
		if h.Enabled(ctx, l) {
			return true
		}
	}
	return false
}

func (m multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m {
		if h.Enabled(ctx, r.Level) {
			_ = h.Handle(ctx, r.Clone())
		}
	}
	return nil
}

func (m multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(multiHandler, len(m))
	for i, h := range m {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (m multiHandler) WithGroup(name string) slog.Handler {
	out := make(multiHandler, len(m))
	for i, h := range m {
		out[i] = h.WithGroup(name)
	}
	return out
}

func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Setup builds the process logger and installs it as slog's default.
func Setup(level string, w io.Writer) (*slog.Logger, *Ring) {
	if w == nil {
		w = os.Stdout
	}
	lv := ParseLevel(level)
	ring := NewRing(2000)
	h := multiHandler{
		slog.NewTextHandler(w, &slog.HandlerOptions{Level: lv}),
		&ringHandler{ring: ring, level: lv},
	}
	l := slog.New(h)
	slog.SetDefault(l)
	return l, ring
}
