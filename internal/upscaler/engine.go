package upscaler

import (
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Asion001/mangarr/internal/version"
)

// EngineConfig configures the upscaling engine on this machine, wherever
// it runs: in the server, or on a worker with the upscale role.
type EngineConfig struct {
	ToolsDir string
	GPU      string
	Threads  string
	Tile     int
	Timeout  time.Duration
	TmpDir   string
	CWebP    string
}

// EngineVars documents the engine's variables (new name, old name, default).
var EngineVars = [][3]string{
	{"MANGARR_UPSCALER_TOOLS_DIR", "UPSCALER_TOOLS_DIR", "/opt/upscalers"},
	{"MANGARR_UPSCALER_GPU", "UPSCALER_GPU", "auto"},
	{"MANGARR_UPSCALER_THREADS", "UPSCALER_THREADS", ""},
	{"MANGARR_UPSCALER_TILE", "UPSCALER_TILE", "0"},
	{"MANGARR_UPSCALER_TIMEOUT", "UPSCALER_TIMEOUT", "30m"},
	{"MANGARR_UPSCALER_TMP_DIR", "UPSCALER_TMP_DIR", ""},
	{"MANGARR_UPSCALER_CWEBP", "UPSCALER_CWEBP", ""},
}

// LoadEngineConfig reads them (the old UPSCALER_* names still work).
func LoadEngineConfig(getenv func(string) string) (EngineConfig, error) {
	get := func(name string) string {
		for _, v := range EngineVars {
			if v[0] == name {
				if x := getenv(v[0]); x != "" {
					return x
				}
				if v[1] != "" {
					if x := getenv(v[1]); x != "" {
						return x
					}
				}
				return v[2]
			}
		}
		return ""
	}
	c := EngineConfig{ToolsDir: get("MANGARR_UPSCALER_TOOLS_DIR"), GPU: get("MANGARR_UPSCALER_GPU"),
		Threads: get("MANGARR_UPSCALER_THREADS"), TmpDir: get("MANGARR_UPSCALER_TMP_DIR"), CWebP: get("MANGARR_UPSCALER_CWEBP")}
	var err error
	if c.Tile, err = strconv.Atoi(get("MANGARR_UPSCALER_TILE")); err != nil {
		return c, fmt.Errorf("MANGARR_UPSCALER_TILE: %w", err)
	}
	if c.Timeout, err = time.ParseDuration(get("MANGARR_UPSCALER_TIMEOUT")); err != nil {
		return c, fmt.Errorf("MANGARR_UPSCALER_TIMEOUT: %w", err)
	}
	return c, nil
}

// NewEngine builds the upscaling engine for c.
func NewEngine(c EngineConfig, log *slog.Logger) *Server {
	runner := CLIRunner{ToolsDir: c.ToolsDir, GPU: c.GPU, Threads: c.Threads, Tile: c.Tile, Log: log}
	return NewServer(Config{TmpDir: c.TmpDir, CWebP: c.CWebP, Timeout: c.Timeout, Version: version.Version}, runner, log)
}
