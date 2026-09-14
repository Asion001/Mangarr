// Package fakelibrary is an in-memory library module (Komga/Kavita stand-in)
// for tests: it records rescans and serves configured per-user progress.
package fakelibrary

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/library"
)

type Settings struct {
	Scenario string `json:"scenario" required:"true"`
}

type Scenario struct {
	mu       sync.Mutex
	Rescans  [][]string
	Progress map[string][]library.BookProgress // api key -> progress
	// Check is returned by VerifyBook; Verified records the paths asked about.
	Check    library.BookCheck
	Verified []string
	// Written records WriteProgress calls (api key -> progress); Known, when
	// set, limits which paths the server "has scanned" (others are missing).
	Written map[string][]library.BookProgress
	Known   map[string]bool
}

var (
	mu        sync.Mutex
	scenarios = map[string]*Scenario{}
)

func NewScenario(name string) *Scenario {
	s := &Scenario{Progress: map[string][]library.BookProgress{}}
	mu.Lock()
	scenarios[name] = s
	mu.Unlock()
	return s
}

func (s *Scenario) SetProgress(key string, p []library.BookProgress) {
	s.mu.Lock()
	s.Progress[key] = p
	s.mu.Unlock()
}

func (s *Scenario) RescanCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Rescans)
}

func init() {
	modules.Register(&modules.Implementation{
		Kind: modules.KindLibrary, Name: "fakelibrary", DisplayName: "Fake library (tests)",
		Settings: func() any { return &Settings{} },
		New: func(deps modules.Deps, st any) (modules.Instance, error) {
			name := st.(*Settings).Scenario
			mu.Lock()
			sc := scenarios[name]
			mu.Unlock()
			if sc == nil {
				// schema listing builds instances with defaults
				return &Module{sc: &Scenario{Progress: map[string][]library.BookProgress{}}}, nil
			}
			return &Module{sc: sc}, nil
		},
	})
}

type Module struct{ sc *Scenario }

func (m *Module) Test(ctx context.Context) error { return nil }

func (m *Module) Rescan(ctx context.Context, paths []string) error {
	m.sc.mu.Lock()
	m.sc.Rescans = append(m.sc.Rescans, paths)
	m.sc.mu.Unlock()
	return nil
}

func (m *Module) AccountFields() []modules.Field {
	return []modules.Field{{Name: "apiKey", Label: "API key", Type: "password", Secret: true, Required: true}}
}

func (m *Module) TestAccount(ctx context.Context, acc library.Account) (string, error) {
	key := acc.Credentials["apiKey"]
	if key == "" {
		return "", errors.New("api key required")
	}
	return fmt.Sprintf("user-%s", key), nil
}

func (m *Module) ReadProgress(ctx context.Context, acc library.Account, roots []string) ([]library.BookProgress, error) {
	m.sc.mu.Lock()
	defer m.sc.mu.Unlock()
	return append([]library.BookProgress(nil), m.sc.Progress[acc.Credentials["apiKey"]]...), nil
}

// SetCheck sets the VerifyBook answer.
func (s *Scenario) SetCheck(c library.BookCheck) {
	s.mu.Lock()
	s.Check = c
	s.mu.Unlock()
}

// VerifyCount returns how often VerifyBook was called.
func (s *Scenario) VerifyCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Verified)
}

func (m *Module) VerifyBook(_ context.Context, localPath string) (library.BookCheck, error) {
	m.sc.mu.Lock()
	defer m.sc.mu.Unlock()
	m.sc.Verified = append(m.sc.Verified, localPath)
	return m.sc.Check, nil
}

var _ library.Verifier = (*Module)(nil)

func (m *Module) WriteProgress(_ context.Context, acc library.Account, items []library.BookProgress) (int, []string, error) {
	m.sc.mu.Lock()
	defer m.sc.mu.Unlock()
	if m.sc.Written == nil {
		m.sc.Written = map[string][]library.BookProgress{}
	}
	key := acc.Credentials["apiKey"]
	var missing []string
	n := 0
	for _, it := range items {
		if m.sc.Known != nil && !m.sc.Known[it.LocalPath] {
			missing = append(missing, it.LocalPath)
			continue
		}
		m.sc.Written[key] = append(m.sc.Written[key], it)
		n++
	}
	return n, missing, nil
}

// SetKnown limits the paths WriteProgress accepts.
func (s *Scenario) SetKnown(paths ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Known = map[string]bool{}
	for _, p := range paths {
		s.Known[p] = true
	}
}

// WrittenFor returns the progress written for an api key.
func (s *Scenario) WrittenFor(key string) []library.BookProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]library.BookProgress(nil), s.Written[key]...)
}

var _ library.ProgressWriter = (*Module)(nil)
