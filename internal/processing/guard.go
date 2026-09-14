package processing

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/library"
	"github.com/Asion001/mangarr/internal/settings"
)

// StateKey stores the Guard state in the settings table.
const StateKey = "processing_state"

// State records which library servers read which re-encoded formats, and
// whether encoding is paused because one couldn't.
type State struct {
	EncodeBlocked bool   `json:"encodeBlocked"`
	Reason        string `json:"reason,omitempty"`
	// Verified maps "<moduleId>:<format>" to "ok".
	Verified map[string]string `json:"verified,omitempty"`
}

// Guard checks, after the first re-encoded chapter of each format, that
// every library server that can report it (Komga) actually read the file.
// If one couldn't, encoding pauses until the user resumes it.
type Guard struct {
	settings *settings.Store
	mods     *modules.Manager
	bus      *events.Bus
	log      *slog.Logger
	// Delay before the first check (rescans are debounced), Interval between
	// checks and Timeout for the server to analyze the file.
	Delay, Interval, Timeout time.Duration

	mu      sync.Mutex
	pending map[string]bool
}

func NewGuard(st *settings.Store, mods *modules.Manager, bus *events.Bus, log *slog.Logger) *Guard {
	g := &Guard{settings: st, mods: mods, bus: bus, log: log, Delay: 90 * time.Second, Interval: 30 * time.Second, Timeout: 10 * time.Minute,
		pending: map[string]bool{}}
	bus.Subscribe(func(e events.Event) {
		if p, ok := e.Payload.(downloads.EncodedPayload); ok {
			g.check(p.Path, p.Format)
		}
	}, downloads.EventFileEncoded)
	return g
}

// State returns the current state.
func (g *Guard) State(ctx context.Context) State {
	var s State
	_ = g.settings.Get(ctx, StateKey, &s)
	if s.Verified == nil {
		s.Verified = map[string]string{}
	}
	return s
}

// Blocked reports whether encoding is paused.
func (g *Guard) Blocked() (bool, string) {
	s := g.State(context.Background())
	return s.EncodeBlocked, s.Reason
}

// Resume clears a pause and forgets verifications (they run again).
func (g *Guard) Resume(ctx context.Context) error {
	err := g.settings.Set(ctx, StateKey, State{})
	g.bus.Changed("processing", "resumed", 0)
	return err
}

func (g *Guard) update(fn func(*State)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s := g.State(context.Background())
	fn(&s)
	if err := g.settings.Set(context.Background(), StateKey, s); err != nil {
		g.log.Warn("save processing state", "err", err)
	}
	g.bus.Changed("processing", "updated", 0)
}

func (g *Guard) check(localPath, format string) {
	state := g.State(context.Background())
	for _, l := range g.mods.Active(modules.KindLibrary) {
		v, ok := modules.As[library.Verifier](l)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%d:%s", l.Def.ID, format)
		g.mu.Lock()
		skip := state.Verified[key] != "" || g.pending[key]
		if !skip {
			g.pending[key] = true
		}
		g.mu.Unlock()
		if skip {
			continue
		}
		go g.verify(key, l.Def.Name, v, localPath, format)
	}
}

func (g *Guard) verify(key, name string, v library.Verifier, localPath, format string) {
	defer func() {
		g.mu.Lock()
		delete(g.pending, key)
		g.mu.Unlock()
	}()
	time.Sleep(g.Delay)
	deadline := time.Now().Add(g.Timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		bc, err := v.VerifyBook(ctx, localPath)
		cancel()
		switch {
		case err != nil:
			g.log.Warn("verify re-encoded file", "server", name, "err", err)
		case bc.Problem != "":
			reason := fmt.Sprintf("%s can't read %s pages: %s", name, format, bc.Problem)
			g.log.Error("pausing re-encoding", "reason", reason, "file", localPath)
			g.update(func(s *State) { s.EncodeBlocked, s.Reason = true, reason })
			g.bus.Publish(events.Event{Type: events.HealthIssue, Payload: events.MessagePayload{Title: "Re-encoding paused",
				Message: reason + ". Fix the server (e.g. use Komga's official amd64/arm64 image) and resume re-encoding in Settings → Profiles."}})
			return
		case bc.Found && bc.Status == "READY":
			g.log.Info("library server reads re-encoded pages", "server", name, "format", format, "width", bc.PageWidth)
			g.update(func(s *State) {
				if s.Verified == nil {
					s.Verified = map[string]string{}
				}
				s.Verified[key] = "ok"
			})
			return
		}
		time.Sleep(g.Interval)
	}
	g.log.Warn("library server didn't analyze the re-encoded file in time", "server", name, "file", localPath)
}
