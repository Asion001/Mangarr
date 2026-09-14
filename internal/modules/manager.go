package modules

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/model"
)

// Loaded is a provider definition plus its built instance (or build error).
type Loaded struct {
	Def  model.ProviderDefinition
	Impl *Implementation
	// Instance is the instance as used by the core, possibly wrapped by a
	// decorator (e.g. request throttling); Raw is the module's own instance.
	Instance Instance
	Raw      Instance
	Err      error
}

// As returns the loaded instance as T, trying the decorated instance first
// and then the raw one (decorators don't forward optional capabilities).
func As[T any](l *Loaded) (T, bool) {
	if t, ok := l.Instance.(T); ok {
		return t, true
	}
	t, ok := l.Raw.(T)
	return t, ok
}

// Decorator wraps instances of one kind as they are built.
type Decorator func(def model.ProviderDefinition, inst Instance) Instance

// Manager builds and caches instances of all provider definitions.
type Manager struct {
	db      *db.DB
	http    *http.Client
	log     *slog.Logger
	dataDir string

	mu     sync.RWMutex
	loaded map[int64]*Loaded
	// onChange is called after definitions change (e.g. to refresh health).
	onChange   []func()
	envLocks   map[string]*EnvLock
	decorators map[Kind][]Decorator
}

func NewManager(d *db.DB, httpClient *http.Client, log *slog.Logger, dataDir string) *Manager {
	return &Manager{db: d, http: httpClient, log: log, dataDir: dataDir, loaded: map[int64]*Loaded{}}
}

func (m *Manager) OnChange(fn func()) { m.onChange = append(m.onChange, fn) }

// Decorate registers a wrapper for instances of kind. Register decorators
// before the first Reload.
func (m *Manager) Decorate(kind Kind, fn Decorator) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.decorators == nil {
		m.decorators = map[Kind][]Decorator{}
	}
	m.decorators[kind] = append(m.decorators[kind], fn)
}

// Reload rebuilds every instance from the database.
func (m *Manager) Reload(ctx context.Context) error {
	var defs []model.ProviderDefinition
	if err := m.db.NewSelect().Model(&defs).Order("id").Scan(ctx); err != nil {
		return err
	}
	next := map[int64]*Loaded{}
	for _, d := range defs {
		next[d.ID] = m.build(d)
	}
	m.mu.Lock()
	old := m.loaded
	m.loaded = next
	m.mu.Unlock()
	for _, l := range old {
		if c, ok := l.Raw.(Closer); ok {
			_ = c.Close()
		}
	}
	for _, fn := range m.onChange {
		fn()
	}
	return nil
}

func (m *Manager) build(d model.ProviderDefinition) *Loaded {
	l := &Loaded{Def: d}
	impl, ok := Lookup(Kind(d.Kind), d.Implementation)
	if !ok {
		l.Err = fmt.Errorf("unknown implementation %s/%s", d.Kind, d.Implementation)
		return l
	}
	l.Impl = impl
	settings, err := DecodeSettings(impl, d.Settings)
	if err != nil {
		l.Err = err
		return l
	}
	inst, err := impl.New(Deps{
		ID: d.ID, Name: d.Name, HTTP: m.http,
		Log:     m.log.With("module", d.Kind+"/"+d.Implementation, "instance", d.Name),
		DataDir: m.dataDir,
	}, settings)
	if err != nil {
		l.Err = err
		return l
	}
	l.Instance, l.Raw = inst, inst
	m.mu.RLock()
	decs := m.decorators[Kind(d.Kind)]
	m.mu.RUnlock()
	for _, dec := range decs {
		l.Instance = dec(d, l.Instance)
	}
	return l
}

// All returns every loaded definition (including disabled/broken), sorted.
func (m *Manager) All(kind Kind) []*Loaded {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Loaded
	for _, l := range m.loaded {
		if kind == "" || Kind(l.Def.Kind) == kind {
			out = append(out, l)
		}
	}
	sortLoaded(out)
	return out
}

// Active returns enabled, successfully built instances of kind by priority.
func (m *Manager) Active(kind Kind) []*Loaded {
	var out []*Loaded
	for _, l := range m.All(kind) {
		if l.Def.Enabled && l.Instance != nil {
			out = append(out, l)
		}
	}
	return out
}

func (m *Manager) Get(id int64) (*Loaded, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l, ok := m.loaded[id]
	return l, ok
}

func sortLoaded(ls []*Loaded) {
	sort.Slice(ls, func(i, j int) bool {
		if ls[i].Def.Priority != ls[j].Def.Priority {
			return ls[i].Def.Priority < ls[j].Def.Priority
		}
		return ls[i].Def.ID < ls[j].Def.ID
	})
}

// Typed is a loaded instance cast to a kind-specific interface.
type Typed[T any] struct {
	Def      model.ProviderDefinition
	Instance T
}

// ActiveAs returns active instances of kind that implement T.
func ActiveAs[T any](m *Manager, kind Kind) []Typed[T] {
	var out []Typed[T]
	for _, l := range m.Active(kind) {
		if t, ok := As[T](l); ok {
			out = append(out, Typed[T]{Def: l.Def, Instance: t})
		}
	}
	return out
}

// GetAs returns instance id cast to T.
func GetAs[T any](m *Manager, id int64) (T, model.ProviderDefinition, error) {
	var zero T
	l, ok := m.Get(id)
	if !ok {
		return zero, model.ProviderDefinition{}, fmt.Errorf("module instance %d not found", id)
	}
	if l.Err != nil {
		return zero, l.Def, fmt.Errorf("module %q is misconfigured: %w", l.Def.Name, l.Err)
	}
	if !l.Def.Enabled {
		return zero, l.Def, fmt.Errorf("module %q is disabled", l.Def.Name)
	}
	t, ok := As[T](l)
	if !ok {
		return zero, l.Def, fmt.Errorf("module %q does not support this operation", l.Def.Name)
	}
	return t, l.Def, nil
}

// ---- CRUD ------------------------------------------------------------------

var ErrNotFound = errors.New("not found")

// Validate checks a definition (implementation exists, settings valid).
func Validate(def *model.ProviderDefinition) error {
	if !ValidKind(def.Kind) {
		return fmt.Errorf("invalid kind %q", def.Kind)
	}
	impl, ok := Lookup(Kind(def.Kind), def.Implementation)
	if !ok {
		return fmt.Errorf("unknown implementation %q for kind %s", def.Implementation, def.Kind)
	}
	if def.Name == "" {
		def.Name = impl.DisplayName
	}
	if def.Settings == nil {
		def.Settings = map[string]any{}
	}
	if def.Tags == nil {
		def.Tags = []int64{}
	}
	if def.Events == nil {
		def.Events = []string{}
	}
	_, err := DecodeSettings(impl, def.Settings)
	return err
}

func (m *Manager) Create(ctx context.Context, def *model.ProviderDefinition) error {
	if err := Validate(def); err != nil {
		return err
	}
	now := time.Now().UTC()
	def.ID, def.ManagedBy = 0, ""
	def.CreatedAt, def.UpdatedAt = now, now
	if _, err := m.db.NewInsert().Model(def).Exec(ctx); err != nil {
		return err
	}
	return m.Reload(ctx)
}

func (m *Manager) Update(ctx context.Context, def *model.ProviderDefinition) error {
	var stored model.ProviderDefinition
	if err := m.db.NewSelect().Model(&stored).Where("id = ?", def.ID).Scan(ctx); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	def.Kind, def.Implementation, def.ManagedBy = stored.Kind, stored.Implementation, stored.ManagedBy
	if impl, ok := Lookup(Kind(def.Kind), def.Implementation); ok {
		def.Settings = MergeSecrets(impl, def.Settings, stored.Settings)
	}
	if l := m.EnvLockFor(stored); l != nil {
		l.Apply(def)
	}
	if err := Validate(def); err != nil {
		return err
	}
	def.CreatedAt = stored.CreatedAt
	def.UpdatedAt = time.Now().UTC()
	if _, err := m.db.NewUpdate().Model(def).WherePK().Exec(ctx); err != nil {
		return err
	}
	return m.Reload(ctx)
}

func (m *Manager) Delete(ctx context.Context, id int64) error {
	if l, ok := m.Get(id); ok && strings.HasPrefix(l.Def.ManagedBy, "env:") {
		return ErrManaged // nodes may be deleted: they re-register while running
	}
	res, err := m.db.NewDelete().Model((*model.ProviderDefinition)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return m.Reload(ctx)
}

// TestDefinition builds a throwaway instance for def and runs Test. When the
// definition has an ID, masked secrets are filled from the stored copy.
func (m *Manager) TestDefinition(ctx context.Context, def model.ProviderDefinition) error {
	if def.ID != 0 {
		if l, ok := m.Get(def.ID); ok && l.Impl != nil {
			def.Settings = MergeSecrets(l.Impl, def.Settings, l.Def.Settings)
		}
	}
	if err := Validate(&def); err != nil {
		return err
	}
	l := m.build(def)
	if l.Err != nil {
		return l.Err
	}
	defer func() {
		if c, ok := l.Raw.(Closer); ok {
			_ = c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return l.Instance.Test(ctx)
}
