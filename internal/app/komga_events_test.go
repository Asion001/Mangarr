package app_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/komgaapi"
	"github.com/Asion001/mangarr/internal/settings"
)

// sseReader reads Komga server-sent events.
type sseReader struct {
	events chan [2]string // name, data
	done   chan struct{}
}

func openSSE(t *testing.T, ctx context.Context, url, key string) *sseReader {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, "GET", url+"/sse/v1/events", nil)
	req.Header.Set("X-API-Key", key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("sse: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	r := &sseReader{events: make(chan [2]string, 64), done: make(chan struct{})}
	go func() {
		defer close(r.done)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		var name string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				r.events <- [2]string{name, strings.TrimPrefix(line, "data: ")}
			}
		}
	}()
	return r
}

// next waits for the named event and returns its data.
func (r *sseReader) next(t *testing.T, name string) map[string]string {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-r.events:
			if ev[0] != name {
				continue
			}
			var data map[string]string
			if err := json.Unmarshal([]byte(ev[1]), &data); err != nil {
				t.Fatalf("%s data %q: %v", name, ev[1], err)
			}
			return data
		case <-timeout:
			t.Fatalf("no %s event", name)
			return nil
		}
	}
}

// TestKomgaAPIEvents: KMReader's live updates.
func TestKomgaAPIEvents(t *testing.T) {
	f := newKomgaFixture(t, dbtest.DSNs(t)["sqlite"])
	ctx, cancel := context.WithCancel(f.e.Ctx)
	defer cancel()
	sse := openSSE(t, ctx, f.srv.URL, f.key)

	req, _ := http.NewRequest("PATCH", f.srv.URL+"/api/v1/books/"+sid(f.chs[2].ID)+"/read-progress", strings.NewReader(`{"page":3}`))
	req.Header.Set("X-API-Key", f.key)
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 204 {
		t.Fatalf("patch: %v", err)
	}
	if d := sse.next(t, "ReadProgressChanged"); d["bookId"] != sid(f.chs[2].ID) || d["userId"] == "" {
		t.Fatalf("ReadProgressChanged %v", d)
	}
	if d := sse.next(t, "ReadProgressSeriesChanged"); d["seriesId"] != sid(f.ser.ID) {
		t.Fatalf("ReadProgressSeriesChanged %v", d)
	}
	if d := sse.next(t, "SeriesChanged"); d["seriesId"] != sid(f.ser.ID) || d["libraryId"] != sid(f.e.RFID) {
		t.Fatalf("SeriesChanged %v", d)
	}
	req, _ = http.NewRequest("DELETE", f.srv.URL+"/api/v1/books/"+sid(f.chs[0].ID)+"/read-progress", nil)
	req.Header.Set("X-API-Key", f.key)
	_, _ = http.DefaultClient.Do(req)
	if d := sse.next(t, "ReadProgressDeleted"); d["bookId"] != sid(f.chs[0].ID) {
		t.Fatalf("ReadProgressDeleted %v", d)
	}
	f.e.App.Bus.Changed("chapter", "updated", f.chs[3].ID)
	if d := sse.next(t, "BookChanged"); d["bookId"] != sid(f.chs[3].ID) || d["seriesId"] != sid(f.ser.ID) {
		t.Fatalf("BookChanged %v", d)
	}
}

// TestKomgaAPIStopEndsStreams: turning the API off closes open event
// streams at once instead of waiting for them.
func TestKomgaAPIStopEndsStreams(t *testing.T) {
	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	svc := komgaapi.NewService(komgaapi.Deps{DB: e.App.DB, Settings: e.App.Settings, Auth: e.App.Auth, Reading: e.App.Reading, Bus: e.App.Bus,
		Log: e.App.Log}, "127.0.0.1:0")
	rd := settings.DefaultReading()
	rd.Enabled = true
	_ = e.App.Settings.Set(e.Ctx, settings.KeyReading, rd)
	if err := svc.Start(e.Ctx); err != nil {
		t.Fatal(err)
	}
	key, _, err := svc.CreateKey(e.Ctx, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	sse := openSSE(t, e.Ctx, "http://"+svc.Status(e.Ctx).Address, key)
	rd.Enabled = false
	_ = e.App.Settings.Set(e.Ctx, settings.KeyReading, rd)
	start := time.Now()
	svc.Reconcile(e.Ctx)
	select {
	case <-sse.done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream still open")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("stopping took %v", d)
	}
}
