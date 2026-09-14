// Package catalogs tracks the catalogs (individual sources such as
// "MangaDex (EN)") offered by the active source modules. It caches the
// catalog lists and keeps a generation counter that changes whenever the set
// of usable catalogs may have changed, so caches keyed by it never serve
// results from catalogs that were disabled or removed.
package catalogs

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/source"
)

// ListTTL is how long a module's catalog list is reused.
const ListTTL = time.Minute

// Catalog is one source catalog of a module instance.
type Catalog struct {
	ModuleID   int64  `json:"moduleId"`
	ModuleName string `json:"moduleName"`
	source.SourceInfo
}

// Key identifies a catalog across modules.
func (c Catalog) Key() string { return Key(c.ModuleID, c.ID) }

// Key builds the "moduleId:sourceId" key.
func Key(moduleID int64, sourceID string) string {
	return strconv.FormatInt(moduleID, 10) + ":" + sourceID
}

type entry struct {
	at   time.Time
	list []source.SourceInfo
}

// Service caches catalog lists per module.
type Service struct {
	mods *modules.Manager
	bus  *events.Bus

	mu    sync.Mutex
	lists map[int64]entry
	gen   atomic.Int64
}

func New(mods *modules.Manager, bus *events.Bus) *Service {
	s := &Service{mods: mods, bus: bus, lists: map[int64]entry{}}
	s.gen.Store(time.Now().UnixMilli()) // distinct across restarts
	mods.OnChange(func() { s.Invalidate(0) })
	return s
}

// Generation changes whenever the usable catalog set may have changed.
func (s *Service) Generation() int64 { return s.gen.Load() }

// Invalidate drops the cached list of a module (0 = all) and bumps the
// generation.
func (s *Service) Invalidate(moduleID int64) {
	s.mu.Lock()
	if moduleID == 0 {
		s.lists = map[int64]entry{}
	} else {
		delete(s.lists, moduleID)
	}
	s.mu.Unlock()
	g := s.gen.Add(1)
	if s.bus != nil {
		s.bus.Changed("catalogs", "updated", g)
	}
}

// List returns the catalogs of all active source modules, sorted by language
// and name. Modules that fail to list are reported in errs.
func (s *Service) List(ctx context.Context, fresh bool) (out []Catalog, errs []string) {
	for _, m := range modules.ActiveAs[source.Module](s.mods, modules.KindSource) {
		s.mu.Lock()
		e, ok := s.lists[m.Def.ID]
		s.mu.Unlock()
		if !ok || fresh || time.Since(e.at) > ListTTL {
			l, err := m.Instance.Sources(ctx)
			if err != nil {
				errs = append(errs, m.Def.Name+": "+err.Error())
				continue
			}
			e = entry{at: time.Now(), list: l}
			s.mu.Lock()
			s.lists[m.Def.ID] = e
			s.mu.Unlock()
		}
		for _, si := range e.list {
			out = append(out, Catalog{ModuleID: m.Def.ID, ModuleName: m.Def.Name, SourceInfo: si})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Lang != out[j].Lang {
			return out[i].Lang < out[j].Lang
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, errs
}
