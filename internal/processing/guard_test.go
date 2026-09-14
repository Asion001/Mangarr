package processing

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/library"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/testutil/fakelibrary"
)

func waitFor(t *testing.T, what string, fn func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if fn() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestGuardPausesEncodingWhenServerCantRead(t *testing.T) {
	d := dbtest.SQLite(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := settings.NewStore(d)
	_ = st.Warm(ctx)
	bus := events.NewBus()
	mods := modules.NewManager(d, nil, log, t.TempDir())
	sc := fakelibrary.NewScenario("guard")
	sc.SetCheck(library.BookCheck{Found: true, Status: "READY", PageWidth: 1000})
	if err := mods.Create(ctx, &model.ProviderDefinition{Kind: "library", Implementation: "fakelibrary", Name: "Komga", Enabled: true,
		Settings: map[string]any{"scenario": "guard"}}); err != nil {
		t.Fatal(err)
	}
	g := NewGuard(st, mods, bus, log)
	g.Delay, g.Interval = 10*time.Millisecond, 10*time.Millisecond

	// first AVIF chapter: verified once, then remembered
	bus.Publish(events.Event{Type: downloads.EventFileEncoded, Payload: downloads.EncodedPayload{Path: "/m/a.cbz", Format: "avif"}})
	waitFor(t, "verification", func() bool { return len(g.State(ctx).Verified) == 1 })
	bus.Publish(events.Event{Type: downloads.EventFileEncoded, Payload: downloads.EncodedPayload{Path: "/m/b.cbz", Format: "avif"}})
	time.Sleep(100 * time.Millisecond)
	if n := sc.VerifyCount(); n != 1 {
		t.Fatalf("a verified format is not checked again (%d checks)", n)
	}

	// a new format the server can't read pauses encoding
	sc.SetCheck(library.BookCheck{Found: true, Status: "READY", Problem: "no dimensions"})
	bus.Publish(events.Event{Type: downloads.EventFileEncoded, Payload: downloads.EncodedPayload{Path: "/m/c.cbz", Format: "jxl"}})
	waitFor(t, "pause", func() bool { b, _ := g.Blocked(); return b })
	p := &Processor{Guard: g}
	_, err := p.Process(ctx, model.ProfileConfig{Encode: model.EncodeConfig{Format: "avif"}}, nil, t.TempDir())
	var un Unavailable
	if !errors.As(err, &un) || !un.Temporary() {
		t.Fatalf("processing while paused: %v", err)
	}
	if err := g.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if b, _ := g.Blocked(); b || len(g.State(ctx).Verified) != 0 {
		t.Fatal("resume clears the pause and verifications")
	}
}
