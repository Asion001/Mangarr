package upscaler

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/version"
)

// NodeConfig configures a processing node (MANGARR_MODE=upscaler).
type NodeConfig struct {
	Listen   string
	ToolsDir string
	GPU      string
	Threads  string
	Tile     int
	Token    string
	Timeout  time.Duration
	TmpDir   string
	CWebP    string
	// Registration with a mangarr server (optional).
	ServerURL string
	APIKey    string
	Name      string
	// AdvertiseURL is how the server reaches this node.
	AdvertiseURL string
}

// NodeVars documents the node variables (new name, old name, default).
var NodeVars = [][3]string{
	{"MANGARR_UPSCALER_LISTEN", "UPSCALER_LISTEN", ":8788"},
	{"MANGARR_UPSCALER_TOOLS_DIR", "UPSCALER_TOOLS_DIR", "/opt/upscalers"},
	{"MANGARR_UPSCALER_GPU", "UPSCALER_GPU", "auto"},
	{"MANGARR_UPSCALER_THREADS", "UPSCALER_THREADS", ""},
	{"MANGARR_UPSCALER_TILE", "UPSCALER_TILE", "0"},
	{"MANGARR_UPSCALER_TOKEN", "UPSCALER_TOKEN", ""},
	{"MANGARR_UPSCALER_TIMEOUT", "UPSCALER_TIMEOUT", "30m"},
	{"MANGARR_UPSCALER_TMP_DIR", "UPSCALER_TMP_DIR", ""},
	{"MANGARR_UPSCALER_CWEBP", "UPSCALER_CWEBP", ""},
	{"MANGARR_SERVER_URL", "", ""},
	{"MANGARR_API_KEY", "", ""},
	{"MANGARR_NODE_NAME", "", ""},
	{"MANGARR_NODE_URL", "", ""},
}

// LoadNodeConfig reads the node variables (old UPSCALER_* names still work).
func LoadNodeConfig(getenv func(string) string) (NodeConfig, error) {
	get := func(name string) string {
		for _, v := range NodeVars {
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
	c := NodeConfig{Listen: get("MANGARR_UPSCALER_LISTEN"), ToolsDir: get("MANGARR_UPSCALER_TOOLS_DIR"), GPU: get("MANGARR_UPSCALER_GPU"),
		Threads: get("MANGARR_UPSCALER_THREADS"), Token: get("MANGARR_UPSCALER_TOKEN"), TmpDir: get("MANGARR_UPSCALER_TMP_DIR"),
		CWebP: get("MANGARR_UPSCALER_CWEBP"), ServerURL: strings.TrimRight(get("MANGARR_SERVER_URL"), "/"), APIKey: get("MANGARR_API_KEY"),
		Name: get("MANGARR_NODE_NAME"), AdvertiseURL: get("MANGARR_NODE_URL")}
	var err error
	if c.Tile, err = strconv.Atoi(get("MANGARR_UPSCALER_TILE")); err != nil {
		return c, fmt.Errorf("MANGARR_UPSCALER_TILE: %w", err)
	}
	if c.Timeout, err = time.ParseDuration(get("MANGARR_UPSCALER_TIMEOUT")); err != nil {
		return c, fmt.Errorf("MANGARR_UPSCALER_TIMEOUT: %w", err)
	}
	if c.Name == "" {
		c.Name, _ = os.Hostname()
	}
	if c.AdvertiseURL == "" {
		_, port, _ := net.SplitHostPort(c.Listen)
		c.AdvertiseURL = "http://" + c.Name + ":" + port
	}
	if c.ServerURL != "" && c.Token == "" {
		// registered nodes always use a token; the server learns it by heartbeat
		c.Token = randomToken()
	}
	return c, nil
}

func randomToken() string {
	b := make([]byte, 16)
	f, err := os.Open("/dev/urandom")
	if err == nil {
		_, _ = f.Read(b)
		f.Close()
	} else {
		copy(b, []byte(strconv.FormatInt(time.Now().UnixNano(), 16)))
	}
	return hex.EncodeToString(b)
}

// NodeID is a stable id derived from the node name.
func (c NodeConfig) NodeID() string {
	sum := sha1.Sum([]byte(c.Name))
	return hex.EncodeToString(sum[:6])
}

// NewNodeServer builds the worker for c.
func NewNodeServer(c NodeConfig, log *slog.Logger) *Server {
	runner := CLIRunner{ToolsDir: c.ToolsDir, GPU: c.GPU, Threads: c.Threads, Tile: c.Tile}
	return NewServer(Config{Token: c.Token, TmpDir: c.TmpDir, CWebP: c.CWebP, Timeout: c.Timeout, Version: version.Version}, runner, log)
}

// Heartbeat is sent by nodes to the server.
type Heartbeat struct {
	NodeID string `json:"nodeId"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Token  string `json:"token"`
	Info   Info   `json:"info"`
}

// RunNode serves the worker API until ctx ends, registering with the
// server when MANGARR_SERVER_URL is set.
func RunNode(ctx context.Context, c NodeConfig, log *slog.Logger) error {
	srv := NewNodeServer(c, log)
	info := srv.Info()
	names := []string{}
	for _, m := range info.Models {
		names = append(names, m.Name)
	}
	log.Info("mangarr processing node starting", "version", version.Version, "models", names, "devices", info.Devices, "listen", c.Listen)
	if len(names) == 0 {
		log.Warn("no upscaler tools found (the full image includes them on amd64)", "dir", c.ToolsDir)
	}
	if c.Token == "" {
		log.Warn("MANGARR_UPSCALER_TOKEN is not set; anyone who can reach this port can use the GPU")
	}
	if c.ServerURL != "" {
		if c.APIKey == "" {
			return errors.New("MANGARR_SERVER_URL needs MANGARR_API_KEY (Settings → General on the server)")
		}
		go heartbeatLoop(ctx, c, srv, log)
	}
	hs := &http.Server{Addr: c.Listen, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// HeartbeatInterval is how often nodes report to the server.
const HeartbeatInterval = 30 * time.Second

func heartbeatLoop(ctx context.Context, c NodeConfig, srv *Server, log *slog.Logger) {
	client := &http.Client{Timeout: 15 * time.Second}
	failing := false
	for {
		hb := Heartbeat{NodeID: c.NodeID(), Name: c.Name, URL: c.AdvertiseURL, Token: c.Token, Info: srv.Info()}
		body, _ := json.Marshal(hb)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.ServerURL+"/api/v1/upscaler-nodes/heartbeat", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", c.APIKey)
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode >= 300 {
				err = fmt.Errorf("server answered %s", resp.Status)
			}
		}
		switch {
		case err != nil && !failing:
			log.Warn("registering with the mangarr server failed; retrying", "server", c.ServerURL, "err", err)
			failing = true
		case err == nil && failing:
			log.Info("registered with the mangarr server", "server", c.ServerURL, "as", c.AdvertiseURL)
			failing = false
		case err == nil && !failing:
			log.Debug("heartbeat sent")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(HeartbeatInterval):
		}
	}
}
