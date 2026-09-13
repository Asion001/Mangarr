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
}

func Load() (*Config, error) {
	c := &Config{
		Listen:   env("MANGARR_LISTEN", ":8787"),
		DataDir:  env("MANGARR_DATA_DIR", "./config"),
		LogLevel: env("MANGARR_LOG_LEVEL", "info"),
		URLBase:  strings.TrimRight(env("MANGARR_URL_BASE", ""), "/"),
		WebDir:   env("MANGARR_WEB_DIR", ""),
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
