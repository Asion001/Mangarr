package api_test

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogDownloadIsRedacted(t *testing.T) {
	srv, a := newServer(t, true)
	g, _ := a.Settings.General(context.Background())
	slog.Error("request", "url", "http://x/api?apikey="+g.APIKey, "note", "key "+g.APIKey) // the test ring keeps errors

	get := func(url string) []byte {
		resp, err := http.Get(srv.URL + url)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("%s: %v %v", url, err, resp.StatusCode)
		}
		b, _ := io.ReadAll(resp.Body)
		return b
	}
	unzip := func(b []byte) string {
		zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
		if err != nil {
			t.Fatal(err)
		}
		var all strings.Builder
		for _, f := range zr.File {
			rc, _ := f.Open()
			c, _ := io.ReadAll(rc)
			rc.Close()
			all.Write(c)
		}
		return all.String()
	}

	// without log files: the in-memory entries
	text := unzip(get("/api/v1/system/logs/download"))
	if !strings.Contains(text, "request") || strings.Contains(text, g.APIKey) {
		t.Fatalf("recent log:\n%s", text)
	}

	// with log files
	dir := t.TempDir()
	a.Cfg.LogDir = dir
	_ = os.WriteFile(filepath.Join(dir, "mangarr.txt"), []byte("time=now msg=hello key="+g.APIKey+"\n"), 0o644)
	text = unzip(get("/api/v1/system/logs/download"))
	if !strings.Contains(text, "hello") || strings.Contains(text, g.APIKey) {
		t.Fatalf("file log:\n%s", text)
	}
	one := string(get("/api/v1/system/logs/download?file=mangarr.txt"))
	if !strings.Contains(one, "hello") || strings.Contains(one, g.APIKey) {
		t.Fatalf("single file:\n%s", one)
	}
	if resp, _ := http.Get(srv.URL + "/api/v1/system/logs/download?file=../t.db"); resp.StatusCode != 404 {
		t.Fatalf("path traversal: %d", resp.StatusCode)
	}
}

func TestDiagnosticsIsRedacted(t *testing.T) {
	srv, a := newServer(t, true)
	g, _ := a.Settings.General(context.Background())
	slog.Error("oops", "key", g.APIKey)
	resp, err := http.Get(srv.URL + "/api/v1/system/diagnostics")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("%v %v", err, resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		rc, _ := f.Open()
		c, _ := io.ReadAll(rc)
		rc.Close()
		if strings.Contains(string(c), g.APIKey) || strings.Contains(string(c), g.SessionSecret) {
			t.Fatalf("%s contains a secret", f.Name)
		}
	}
	for _, n := range []string{"system.json", "health.json", "modules.json", "settings.json", "env.json", "queue.json", "logs/recent.txt"} {
		if !names[n] {
			t.Fatalf("missing %s in %v", n, names)
		}
	}
}
