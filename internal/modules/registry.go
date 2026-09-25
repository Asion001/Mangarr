// Package modules is the plug-in system of mangarr. Every external system
// (sources, metadata providers, library servers, notification targets,
// upscalers) is an Implementation registered here. Users configure Instances
// of implementations; each instance is stored as a model.ProviderDefinition.
//
// Only packages under internal/modules/<kind>/<impl> may talk to vendor APIs.
package modules

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
)

type Kind string

const (
	KindSource      Kind = "source"
	KindMetadata    Kind = "metadata"
	KindLibrary     Kind = "library"
	KindNotify      Kind = "notify"
	KindUpscale     Kind = "upscale"
	KindMediaServer Kind = "mediaserver"
)

var Kinds = []Kind{KindSource, KindMetadata, KindLibrary, KindNotify, KindUpscale, KindMediaServer}

func ValidKind(k string) bool {
	for _, x := range Kinds {
		if string(x) == k {
			return true
		}
	}
	return false
}

// Instance is a configured, ready-to-use module. Kind-specific interfaces
// (source.Module, notify.Module, ...) embed it.
type Instance interface {
	// Test verifies connectivity/credentials.
	Test(ctx context.Context) error
}

// Closer is implemented by instances holding resources.
type Closer interface{ Close() error }

// Deps are handed to implementations when an instance is built.
type Deps struct {
	ID      int64
	Name    string
	HTTP    *http.Client
	Log     *slog.Logger
	DataDir string
}

type Implementation struct {
	Kind        Kind
	Name        string // stable identifier, e.g. "suwayomi"
	DisplayName string
	Description string
	InfoURL     string
	// Settings returns a pointer to a settings struct populated with defaults.
	Settings func() any
	// New builds an instance from a settings value produced by Settings().
	New func(deps Deps, settings any) (Instance, error)
}

var (
	regMu    sync.RWMutex
	registry = map[Kind]map[string]*Implementation{}
)

// Register adds an implementation. Called from init() of implementation packages.
func Register(impl *Implementation) {
	regMu.Lock()
	defer regMu.Unlock()
	if impl.Settings == nil || impl.New == nil {
		panic(fmt.Sprintf("module %s/%s: Settings and New are required", impl.Kind, impl.Name))
	}
	if registry[impl.Kind] == nil {
		registry[impl.Kind] = map[string]*Implementation{}
	}
	if _, dup := registry[impl.Kind][impl.Name]; dup {
		panic(fmt.Sprintf("module %s/%s registered twice", impl.Kind, impl.Name))
	}
	registry[impl.Kind][impl.Name] = impl
}

// Lookup returns an implementation by kind and name.
func Lookup(kind Kind, name string) (*Implementation, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	impl, ok := registry[kind][name]
	return impl, ok
}

// Implementations lists registered implementations of a kind, sorted by name.
func Implementations(kind Kind) []*Implementation {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]*Implementation, 0, len(registry[kind]))
	for _, impl := range registry[kind] {
		out = append(out, impl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// HealthChecker is implemented by instances with a cheap health probe. A
// non-empty warning is shown without failing the check.
type HealthChecker interface {
	HealthCheck(ctx context.Context) (warning string, err error)
}
