package app

import (
	"context"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/komgaapi"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/reading"
	"github.com/Asion001/mangarr/internal/readsync"
)

// Progress fan-out delays: changes are batched per series (a reader turning
// pages sends one update every few seconds); unreads go out sooner so a
// server's own sync doesn't bring them back first.
const (
	FanOutDelay       = 30 * time.Second
	FanOutUnreadDelay = 3 * time.Second
)

// wireReading sets up what reading apps see, the Komga-compatible API and
// mangarr as the progress hub between apps and library servers.
func (a *App) wireReading(ctx context.Context) error {
	a.Reading = &reading.Service{DB: a.DB, Settings: a.Settings, Library: a.Library, ImageCache: a.ImageCache, Mods: a.Modules,
		HTTP: a.HTTP, Bus: a.Bus, Downloads: a.Searcher, Log: a.Log.With("component", "reading")}
	a.Komga = komgaapi.NewService(komgaapi.Deps{DB: a.DB, Settings: a.Settings, Auth: a.Auth, Reading: a.Reading, Bus: a.Bus,
		Log: a.Log.With("component", "komga-api")}, a.Cfg.KomgaListen)
	a.AddService(a.Komga)

	// what library servers report is logged per server and announced like
	// app progress
	a.ReadSync.OnChange = func(ctx context.Context, acc *model.ReaderAccount, changes []readsync.Change) {
		name := "Library server"
		if l, ok := a.Modules.Get(acc.ModuleID); ok {
			name = l.Def.Name
		}
		out := make([]reading.Outcome, len(changes))
		for i, c := range changes {
			out[i] = reading.Outcome{Change: reading.Change{ChapterID: c.ChapterID, SeriesID: c.SeriesID, Completed: c.Completed, Page: c.Page,
				Unread: c.Outcome == model.OutcomeUnread}, Result: c.Outcome}
		}
		a.Reading.Observe(ctx, acc.ReaderID, out, reading.By{Origin: model.EventOriginServer, Client: name})
	}

	// every change reaches the other library servers
	a.FanOut = &FanOut{a: a, pending: map[int64]*fanOutSeries{}, delay: FanOutDelay, unreadDelay: FanOutUnreadDelay}
	a.Bus.Subscribe(func(e events.Event) {
		if p, ok := e.Payload.(reading.ProgressPayload); ok && e.SeriesID > 0 {
			a.FanOut.schedule(e.SeriesID, p)
		}
	}, reading.ProgressChanged)
	return nil
}

// FanOut pushes progress changes to library servers, batched per series.
type FanOut struct {
	a                  *App
	mu                 sync.Mutex
	pending            map[int64]*fanOutSeries
	delay, unreadDelay time.Duration
}

// SetDelays changes the batching delays (tests).
func (f *FanOut) SetDelays(delay, unread time.Duration) {
	f.mu.Lock()
	f.delay, f.unreadDelay = delay, unread
	f.mu.Unlock()
}

type fanOutSeries struct {
	timer  *time.Timer
	due    time.Time
	unread map[int64]bool
}

func (f *FanOut) schedule(seriesID int64, p reading.ProgressPayload) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ps := f.pending[seriesID]
	if ps == nil {
		ps = &fanOutSeries{unread: map[int64]bool{}}
		f.pending[seriesID] = ps
	}
	delay := f.delay
	if p.Deleted {
		delay = f.unreadDelay
		for _, c := range p.ChapterIDs {
			ps.unread[c] = true
		}
	}
	due := time.Now().Add(delay)
	if ps.timer != nil {
		if !due.Before(ps.due) {
			return // already due sooner
		}
		ps.timer.Stop()
	}
	ps.due = due
	ps.timer = time.AfterFunc(delay, func() { f.push(seriesID) })
}

func (f *FanOut) push(seriesID int64) {
	f.mu.Lock()
	ps := f.pending[seriesID]
	delete(f.pending, seriesID)
	f.mu.Unlock()
	if ps == nil {
		return
	}
	unread := make([]int64, 0, len(ps.unread))
	for c := range ps.unread {
		unread = append(unread, c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	res, err := f.a.ReadSync.PushSeries(ctx, seriesID, unread)
	switch {
	case err != nil:
		f.a.Log.Warn("progress not pushed to library servers", "series", seriesID, "err", err)
	case res.Written > 0:
		f.a.Log.Info("progress pushed to library servers", "series", seriesID, "books", res.Written, "unknownToServer", res.Missing)
	}
}
