package modules

import (
	"errors"

	"github.com/Asion001/mangarr/internal/model"
)

// ErrManaged is returned when deleting a definition that comes from the
// environment.
var ErrManaged = errors.New("this module is defined by environment variables; remove them to manage it here")

// EnvLock describes which parts of a definition are pinned by environment
// variables (see internal/envcfg). Pinned values win over edits from the UI.
type EnvLock struct {
	// Fields maps settings field names to their variable name.
	Fields map[string]string `json:"fields"`
	// Values holds the pinned settings values.
	Values map[string]any `json:"-"`
	// Meta maps "name", "enabled", "priority", "tags", "events" to their variables.
	Meta map[string]string `json:"meta"`

	Name     *string  `json:"-"`
	Enabled  *bool    `json:"-"`
	Priority *int     `json:"-"`
	Tags     []int64  `json:"-"`
	Events   []string `json:"-"`
}

// Apply writes the pinned values into def.
func (l *EnvLock) Apply(def *model.ProviderDefinition) {
	if def.Settings == nil {
		def.Settings = map[string]any{}
	}
	for k, v := range l.Values {
		def.Settings[k] = v
	}
	if l.Name != nil {
		def.Name = *l.Name
	}
	if l.Enabled != nil {
		def.Enabled = *l.Enabled
	}
	if l.Priority != nil {
		def.Priority = *l.Priority
	}
	if l.Tags != nil {
		def.Tags = l.Tags
	}
	if l.Events != nil {
		def.Events = l.Events
	}
}

// SetEnvLocks replaces the locks, keyed by ProviderDefinition.ManagedBy.
func (m *Manager) SetEnvLocks(locks map[string]*EnvLock) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.envLocks = locks
}

// EnvLockFor returns the lock of a managed definition (nil if unmanaged).
func (m *Manager) EnvLockFor(def model.ProviderDefinition) *EnvLock {
	if def.ManagedBy == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.envLocks[def.ManagedBy]
}
