package komga

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/modules/httpx"
	"github.com/Asion001/mangarr/internal/modules/library"
)

// IdleTimeout ends a stream that sent nothing (Komga sends a heartbeat
// every 15 seconds).
var IdleTimeout = 45 * time.Second

type bookDTO struct {
	ID           string `json:"id"`
	SeriesID     string `json:"seriesId"`
	URL          string `json:"url"`
	ReadProgress *struct {
		Page      int        `json:"page"`
		Completed bool       `json:"completed"`
		ReadDate  *time.Time `json:"readDate"`
	} `json:"readProgress"`
}

// seriesDirs caches series id -> local folder (admin lookups).
type seriesDirs struct {
	mu     sync.Mutex
	dirs   map[string]string
	at     map[string]time.Time
	listed time.Time // last full series listing (for SeriesURL)
}

func (m *Module) seriesDir(ctx context.Context, id string) (string, error) {
	m.dirsOnce.Do(func() { m.dirs = &seriesDirs{dirs: map[string]string{}, at: map[string]time.Time{}} })
	d := m.dirs
	d.mu.Lock()
	dir, ok := d.dirs[id]
	fresh := time.Since(d.at[id]) < 10*time.Minute
	d.mu.Unlock()
	if ok && fresh {
		return dir, nil
	}
	var s komgaSeries
	if err := m.do(ctx, m.s.APIKey, http.MethodGet, "/api/v1/series/"+id, nil, &s); err != nil {
		return "", err
	}
	dir = m.pm.ToLocal(s.URL)
	d.mu.Lock()
	d.dirs[id], d.at[id] = dir, time.Now()
	d.mu.Unlock()
	return dir, nil
}

// book fetches a book with the reader's key and maps it to a local path.
func (m *Module) book(ctx context.Context, key, id string) (*library.BookProgress, error) {
	var b bookDTO
	if err := m.do(ctx, key, http.MethodGet, "/api/v1/books/"+id, nil, &b); err != nil {
		return nil, err
	}
	dir, err := m.seriesDir(ctx, b.SeriesID)
	if err != nil {
		return nil, err
	}
	// non-admin users only see the file name; join with the series folder
	bp := &library.BookProgress{LocalPath: path.Join(dir, path.Base(strings.ReplaceAll(b.URL, "\\", "/")))}
	if b.ReadProgress != nil {
		bp.Completed, bp.Page, bp.ReadAt = b.ReadProgress.Completed, b.ReadProgress.Page, b.ReadProgress.ReadDate
	}
	return bp, nil
}

// WatchProgress follows Komga's server-sent events for the reader.
func (m *Module) WatchProgress(ctx context.Context, acc library.Account, onEvent func(library.ProgressEvent)) error {
	key := acc.Credentials["apiKey"]
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, err := http.NewRequestWithContext(sctx, http.MethodGet, httpx.Join(m.s.URL, "/sse/v1/events"), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", key)
	req.Header.Set("Accept", "text/event-stream")
	// no client timeout: the stream stays open; idleness is checked below
	client := &http.Client{Transport: m.http.Transport}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("komga events: %s", resp.Status)
	}
	onEvent(library.ProgressEvent{Resync: true}) // catch up on anything missed

	lines := make(chan string)
	readErr := make(chan error, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-sctx.Done():
				return
			}
		}
		err := sc.Err()
		if err == nil {
			err = errors.New("komga closed the event stream")
		}
		readErr <- err
	}()

	idle := time.NewTimer(IdleTimeout)
	defer idle.Stop()
	var event, data string
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			return err
		case <-idle.C:
			return errors.New("komga event stream went quiet")
		case line := <-lines:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(IdleTimeout)
			switch {
			case line == "":
				if event != "" {
					m.handleEvent(sctx, key, event, data, onEvent)
				}
				event, data = "", ""
			case strings.HasPrefix(line, ":"): // heartbeat comment
			case strings.HasPrefix(line, "event:"):
				event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}
}

func (m *Module) handleEvent(ctx context.Context, key, event, data string, onEvent func(library.ProgressEvent)) {
	switch event {
	case "ReadProgressChanged", "ReadProgressDeleted":
		var ev struct {
			BookID string `json:"bookId"`
		}
		if json.Unmarshal([]byte(data), &ev) != nil || ev.BookID == "" {
			return
		}
		bctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		bp, err := m.book(bctx, key, ev.BookID)
		if err != nil {
			onEvent(library.ProgressEvent{Resync: true}) // fall back to a full sync
			return
		}
		deleted := event == "ReadProgressDeleted" || (!bp.Completed && bp.Page == 0)
		onEvent(library.ProgressEvent{Book: bp, Deleted: deleted})
	case "ReadProgressSeriesChanged", "ReadProgressSeriesDeleted":
		onEvent(library.ProgressEvent{Resync: true})
	}
}

// SeriesURL links a local series folder to Komga's web UI.
func (m *Module) SeriesURL(ctx context.Context, localDir string) (string, error) {
	m.dirsOnce.Do(func() { m.dirs = &seriesDirs{dirs: map[string]string{}, at: map[string]time.Time{}} })
	d := m.dirs
	d.mu.Lock()
	stale := time.Since(d.listed) > 10*time.Minute
	d.mu.Unlock()
	if stale {
		var series pageOf[komgaSeries]
		if err := m.do(ctx, m.s.APIKey, http.MethodGet, "/api/v1/series?unpaged=true", nil, &series); err != nil {
			return "", err
		}
		d.mu.Lock()
		now := time.Now()
		for _, s := range series.Content {
			d.dirs[s.ID], d.at[s.ID] = m.pm.ToLocal(s.URL), now
		}
		d.listed = now
		d.mu.Unlock()
	}
	want := path.Clean(localDir)
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, dir := range d.dirs {
		if path.Clean(dir) == want {
			return httpx.Join(m.s.URL, "/series/"+id), nil
		}
	}
	return "", nil
}

var (
	_ library.ProgressWatcher = (*Module)(nil)
	_ library.WebLinker       = (*Module)(nil)
)
