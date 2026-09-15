package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Asion001/mangarr/internal/access"
	"github.com/Asion001/mangarr/internal/events"
)

// handleEvents streams bus events to the UI as Server-Sent Events.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan events.Event, 256)
	// people who aren't admins only get cache invalidation (no titles of
	// series outside what they may see, no system events)
	admin := access.From(r.Context()).IsAdmin()
	unsub := s.app.Bus.Subscribe(func(e events.Event) {
		if !admin && e.Type != events.ResourceChanged {
			return
		}
		select {
		case ch <- e:
		default: // slow client: drop; the UI refetches on reconnect
		}
	})
	defer unsub()

	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case e := <-ch:
			b, err := json.Marshal(e)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
			flusher.Flush()
		}
	}
}
