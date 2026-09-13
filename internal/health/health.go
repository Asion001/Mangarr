// Package health runs Sonarr-style health checks and publishes
// health.issue / health.restored events on transitions.
package health

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/fsutil"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/settings"
)

const (
	Notice  = "notice"
	Warning = "warning"
	Error   = "error"
)

type Check struct {
	Source  string `json:"source"`
	Type    string `json:"type"`
	Message string `json:"message"`
	Link    string `json:"link,omitempty"`
}

func (c Check) key() string { return c.Source + "|" + c.Message }

// StatusProvider exposes failing instances of a subsystem (notifications, rescans).
type StatusProvider interface {
	Status() map[int64]string
}

type Checker struct {
	db       *db.DB
	bus      *events.Bus
	mods     *modules.Manager
	settings *settings.Store
	log      *slog.Logger
	// Extra status sources, keyed by label.
	statuses map[string]func() map[int64]string

	mu      sync.Mutex
	results []Check
	last    map[string]Check
	at      time.Time
}

func New(d *db.DB, bus *events.Bus, mods *modules.Manager, st *settings.Store, log *slog.Logger) *Checker {
	return &Checker{db: d, bus: bus, mods: mods, settings: st, log: log, statuses: map[string]func() map[int64]string{}, last: map[string]Check{}}
}

// AddStatus registers a subsystem whose failing instances become warnings.
func (c *Checker) AddStatus(label string, fn func() map[int64]string) { c.statuses[label] = fn }

// Results returns the latest results.
func (c *Checker) Results() ([]Check, time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Check(nil), c.results...), c.at
}

// Run executes every check and publishes transitions.
func (c *Checker) Run(ctx context.Context) []Check {
	var out []Check
	add := func(ch Check) { out = append(out, ch) }

	c.checkModules(ctx, add)
	c.checkRootFolders(ctx, add)
	c.checkSources(ctx, add)
	c.checkQueue(ctx, add)
	c.checkReaders(ctx, add)
	c.checkUpscale(ctx, add)
	for label, fn := range c.statuses {
		for id, msg := range fn() {
			name := fmt.Sprintf("#%d", id)
			if l, ok := c.mods.Get(id); ok {
				name = l.Def.Name
			}
			add(Check{Source: label, Type: Warning, Message: name + ": " + msg})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i].Type) > rank(out[j].Type) })

	c.mu.Lock()
	prev := c.last
	next := map[string]Check{}
	for _, ch := range out {
		next[ch.key()] = ch
	}
	c.results, c.last, c.at = out, next, time.Now().UTC()
	c.mu.Unlock()

	for k, ch := range next {
		if _, ok := prev[k]; !ok && ch.Type != Notice {
			c.bus.Publish(events.Event{Type: events.HealthIssue, Payload: events.MessagePayload{Title: "Health issue: " + ch.Source, Message: ch.Message}})
		}
	}
	for k, ch := range prev {
		if _, ok := next[k]; !ok && ch.Type != Notice {
			c.bus.Publish(events.Event{Type: events.HealthRestored, Payload: events.MessagePayload{Title: "Health restored: " + ch.Source, Message: ch.Message}})
		}
	}
	c.bus.Changed("health", "sync", 0)
	return out
}

func rank(t string) int {
	switch t {
	case Error:
		return 2
	case Warning:
		return 1
	}
	return 0
}

func (c *Checker) checkModules(ctx context.Context, add func(Check)) {
	if len(c.mods.Active(modules.KindSource)) == 0 {
		add(Check{Source: "Sources", Type: Warning, Message: "No source module is configured; add one under Settings → Sources"})
	}
	if len(c.mods.Active(modules.KindMetadata)) == 0 {
		add(Check{Source: "Metadata", Type: Notice, Message: "No metadata module is configured; series use source metadata only"})
	}
	if len(c.mods.Active(modules.KindLibrary)) == 0 {
		add(Check{Source: "Library servers", Type: Notice, Message: "No library server (Komga/Kavita) is configured; reader apps won't see new chapters until they rescan on their own"})
	}
	for _, l := range c.mods.All("") {
		if !l.Def.Enabled {
			continue
		}
		label := strings.ToUpper(l.Def.Kind[:1]) + l.Def.Kind[1:]
		if l.Err != nil {
			add(Check{Source: label, Type: Error, Message: fmt.Sprintf("%s is misconfigured: %v", l.Def.Name, l.Err)})
			continue
		}
		if l.Def.Kind == string(modules.KindNotify) {
			continue // Test() would send a message; failures come from the dispatcher status
		}
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		var warn string
		var err error
		if hc, ok := l.Instance.(modules.HealthChecker); ok {
			warn, err = hc.HealthCheck(cctx)
		} else {
			err = l.Instance.Test(cctx)
		}
		cancel()
		switch {
		case err != nil:
			add(Check{Source: label, Type: Error, Message: fmt.Sprintf("%s is unavailable: %v", l.Def.Name, err)})
		case warn != "":
			add(Check{Source: label, Type: Warning, Message: fmt.Sprintf("%s: %s", l.Def.Name, warn)})
		}
	}
}

func (c *Checker) checkRootFolders(ctx context.Context, add func(Check)) {
	var roots []model.RootFolder
	if err := c.db.NewSelect().Model(&roots).Scan(ctx); err != nil {
		return
	}
	if len(roots) == 0 {
		add(Check{Source: "Root folders", Type: Warning, Message: "No root folder is configured"})
	}
	mm, _ := c.settings.MediaManagement(ctx)
	for _, r := range roots {
		if err := fsutil.Writable(r.Path); err != nil {
			add(Check{Source: "Root folders", Type: Error, Message: fmt.Sprintf("%s is not writable: %v", r.Path, err)})
			continue
		}
		if free, err := fsutil.FreeSpace(r.Path); err == nil && mm.MinFreeSpaceMB > 0 && free < uint64(mm.MinFreeSpaceMB)*2<<20 {
			add(Check{Source: "Root folders", Type: Warning, Message: fmt.Sprintf("%s is low on space (%d MB free)", r.Path, free>>20)})
		}
	}
}

func (c *Checker) checkSources(ctx context.Context, add func(Check)) {
	type row struct {
		Title      string `bun:"title"`
		SourceName string `bun:"source_name"`
		Failures   int    `bun:"consecutive_failures"`
		LastError  string `bun:"last_error"`
	}
	var failing []row
	_ = c.db.NewSelect().TableExpr("series_sources AS ss").ColumnExpr("s.title, ss.source_name, ss.consecutive_failures, ss.last_error").
		Join("JOIN series AS s ON s.id = ss.series_id").Where("ss.enabled = ? AND ss.consecutive_failures >= 3", true).
		OrderExpr("ss.consecutive_failures DESC").Limit(50).Scan(ctx, &failing)
	bySource := map[string][]string{}
	for _, f := range failing {
		bySource[f.SourceName] = append(bySource[f.SourceName], f.Title)
	}
	for src, titles := range bySource {
		msg := fmt.Sprintf("%s keeps failing for %d series (%s)", src, len(titles), strings.Join(first(titles, 3), ", "))
		add(Check{Source: "Sources", Type: Warning, Message: msg})
	}
	var orphans []string
	_ = c.db.NewSelect().Model((*model.Series)(nil)).Column("title").
		Where("monitored = ? AND NOT EXISTS (SELECT 1 FROM series_sources ss WHERE ss.series_id = series.id AND ss.enabled = ?)", true, true).
		Limit(20).Scan(ctx, &orphans)
	if len(orphans) > 0 {
		add(Check{Source: "Series", Type: Warning, Message: fmt.Sprintf("%d monitored series have no enabled source (%s)", len(orphans), strings.Join(first(orphans, 3), ", "))})
	}
}

func (c *Checker) checkQueue(ctx context.Context, add func(Check)) {
	n, _ := c.db.NewSelect().Model((*model.DownloadJob)(nil)).Where("status = ?", model.JobFailed).
		Where("updated_at > ?", time.Now().UTC().Add(-24*time.Hour)).Count(ctx)
	if n > 0 {
		add(Check{Source: "Downloads", Type: Warning, Message: fmt.Sprintf("%d downloads failed in the last 24 hours", n)})
	}
}

func (c *Checker) checkReaders(ctx context.Context, add func(Check)) {
	var bad []struct {
		Name      string `bun:"name"`
		LastError string `bun:"last_error"`
	}
	_ = c.db.NewSelect().TableExpr("reader_accounts AS a").ColumnExpr("r.name, a.last_error").
		Join("JOIN readers AS r ON r.id = a.reader_id").Where("a.last_error <> ''").Scan(ctx, &bad)
	for _, b := range bad {
		add(Check{Source: "Readers", Type: Warning, Message: fmt.Sprintf("progress sync for %s fails: %s", b.Name, b.LastError)})
	}
}

func (c *Checker) checkUpscale(ctx context.Context, add func(Check)) {
	var profiles []model.Profile
	if err := c.db.NewSelect().Model(&profiles).Scan(ctx); err != nil {
		return
	}
	for _, p := range profiles {
		if p.Config.Upscale.Enabled && len(c.mods.Active(modules.KindUpscale)) == 0 {
			add(Check{Source: "Upscaling", Type: Warning, Message: fmt.Sprintf("profile %q enables upscaling but no upscaler module is configured", p.Name)})
		}
	}
}

func first(xs []string, n int) []string {
	if len(xs) > n {
		return append(xs[:n:n], "…")
	}
	return xs
}
