// Package libsync debounces library changes and asks every active library
// module (Komga, Kavita) to rescan the affected series folders.
package libsync

import (
	"context"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/library"
)

type Rescanner struct {
	mods *modules.Manager
	bus  *events.Bus
	log  *slog.Logger

	Quiet time.Duration
	Max   time.Duration

	mu      sync.Mutex
	pending map[string]bool
	first   time.Time
	timer   *time.Timer
	ctx     context.Context
	status  map[int64]string // instance id -> last error
}

func New(mods *modules.Manager, bus *events.Bus, log *slog.Logger) *Rescanner {
	return &Rescanner{mods: mods, bus: bus, log: log, Quiet: 30 * time.Second, Max: 2 * time.Minute,
		pending: map[string]bool{}, status: map[int64]string{}}
}

func (r *Rescanner) Start(ctx context.Context) error {
	r.ctx = ctx
	r.bus.Subscribe(func(e events.Event) {
		if p, ok := e.Payload.(string); ok && p != "" {
			r.Queue(filepath.Dir(p))
		}
	}, downloads.EventFileWritten)
	return nil
}

// Queue schedules a rescan of a series folder.
func (r *Rescanner) Queue(dirs ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) == 0 {
		r.first = time.Now()
	}
	for _, d := range dirs {
		r.pending[d] = true
	}
	wait := r.Quiet
	if remaining := r.Max - time.Since(r.first); remaining < wait {
		wait = max(remaining, 0)
	}
	if r.timer != nil {
		r.timer.Stop()
	}
	r.timer = time.AfterFunc(wait, r.flush)
}

func (r *Rescanner) flush() {
	r.mu.Lock()
	dirs := make([]string, 0, len(r.pending))
	for d := range r.pending {
		dirs = append(dirs, d)
	}
	r.pending = map[string]bool{}
	r.mu.Unlock()
	if len(dirs) == 0 {
		return
	}
	sort.Strings(dirs)
	ctx := r.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	r.Now(ctx, dirs)
}

// Now rescans synchronously and returns per-instance errors.
func (r *Rescanner) Now(ctx context.Context, dirs []string) map[string]error {
	errs := map[string]error{}
	for _, inst := range modules.ActiveAs[library.Module](r.mods, modules.KindLibrary) {
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		err := inst.Instance.Rescan(sctx, dirs)
		cancel()
		r.mu.Lock()
		if err != nil {
			r.status[inst.Def.ID] = err.Error()
			errs[inst.Def.Name] = err
			r.log.Warn("library rescan failed", "instance", inst.Def.Name, "err", err)
		} else {
			delete(r.status, inst.Def.ID)
			r.log.Debug("library rescan requested", "instance", inst.Def.Name, "folders", len(dirs))
		}
		r.mu.Unlock()
	}
	return errs
}

// Status returns the last rescan error per instance.
func (r *Rescanner) Status() map[int64]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[int64]string{}
	for k, v := range r.status {
		out[k] = v
	}
	return out
}
