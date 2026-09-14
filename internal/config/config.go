// Package config loads process-level configuration from environment variables.
// Runtime-editable settings live in the database (see internal/settings).
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	// Listen address, e.g. ":8787".
	Listen string
	// DataDir holds the SQLite database, staging area, backups, logs and caches.
	DataDir string
	// DB is a DSN: "sqlite:///config/mangarr.db" (default) or "postgres://user:pass@host:5432/db".
	DB string
	// LogLevel: debug, info, warn, error.
	LogLevel string
	// AuthDisabled turns off login + API key checks (for use behind an auth proxy).
	AuthDisabled bool
	// URLBase allows serving under a sub path, e.g. "/mangarr".
	URLBase string
	// WebDir overrides the embedded UI with files from disk (development).
	WebDir string
	// Env is the MANGARR_* environment used to pin settings, root folders and
	// modules (see internal/envcfg). Nil in tests unless set explicitly.
	Env map[string]string
}

// VarDoc documents a process-level variable.
type VarDoc struct{ Name, Default, Description string }

// Vars lists the process-level variables read by Load.
var Vars = []VarDoc{
	{"MANGARR_LISTEN", ":8787", "HTTP listen address."},
	{"MANGARR_DATA_DIR", "./config (/config in Docker)", "Database, staging, backups, recycle bin and caches."},
	{"MANGARR_DB", "sqlite://$MANGARR_DATA_DIR/mangarr.db", "Database DSN: sqlite://… or postgres://user:pass@host:5432/db."},
	{"MANGARR_LOG_LEVEL", "info", "debug, info, warn or error."},
	{"MANGARR_URL_BASE", "", "Serve under a sub path, e.g. /mangarr."},
	{"MANGARR_AUTH_DISABLED", "false", "Disable login and API key checks (only behind an auth proxy)."},
	{"MANGARR_WEB_DIR", "", "Serve the UI from this directory instead of the embedded copy (development)."},
}

func Load() (*Config, error) {
	c := &Config{
		Listen:   env("MANGARR_LISTEN", ":8787"),
		DataDir:  env("MANGARR_DATA_DIR", "./config"),
		LogLevel: env("MANGARR_LOG_LEVEL", "info"),
		URLBase:  strings.TrimRight(env("MANGARR_URL_BASE", ""), "/"),
		WebDir:   env("MANGARR_WEB_DIR", ""),
		Env:      Environ(),
	}
	var err error
	if c.AuthDisabled, err = envBool("MANGARR_AUTH_DISABLED", false); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(c.DataDir)
	if err != nil {
		return nil, fmt.Errorf("data dir: %w", err)
	}
	c.DataDir = abs
	c.DB = env("MANGARR_DB", "sqlite://"+filepath.Join(c.DataDir, "mangarr.db"))
	if c.URLBase != "" && !strings.HasPrefix(c.URLBase, "/") {
		c.URLBase = "/" + c.URLBase
	}
	return c, nil
}

// Environ returns the MANGARR_* variables of the process.
func Environ() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(k, "MANGARR_") {
			out[k] = v
		}
	}
	return out
}

// Dir returns a subdirectory of DataDir, creating it if needed.
func (c *Config) Dir(name string) (string, error) {
	p := filepath.Join(c.DataDir, name)
	if err := os.MkdirAll(p, 0o775); err != nil {
		return "", err
	}
	return p, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}
