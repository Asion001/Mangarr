// Command mangarr is a Sonarr-style PVR for manga.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Asion001/mangarr/internal/api"
	"github.com/Asion001/mangarr/internal/app"
	"github.com/Asion001/mangarr/internal/config"
	"github.com/Asion001/mangarr/internal/envcfg"
	"github.com/Asion001/mangarr/internal/imageenc"
	"github.com/Asion001/mangarr/internal/logging"
	_ "github.com/Asion001/mangarr/internal/modules/all"
	"github.com/Asion001/mangarr/internal/version"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println(version.Version, version.Commit)
			return
		case "openapi":
			if err := dumpOpenAPI(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		case "healthcheck":
			os.Exit(healthcheck())
		case "bench":
			os.Exit(bench(os.Args[2:]))
		case "env":
			if len(os.Args) > 2 && os.Args[2] == "--markdown" {
				envcfg.WriteMarkdown(os.Stdout)
			} else {
				envcfg.WriteText(os.Stdout, config.Environ())
			}
			return
		}
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log, ring := logging.Setup(cfg.LogLevel, os.Stdout)
	log.Info("starting mangarr", "version", version.Version, "data", cfg.DataDir, "listen", cfg.Listen)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.New(ctx, cfg, log, ring)
	if err != nil {
		return err
	}
	defer a.Close()
	if err := a.Start(ctx); err != nil {
		return err
	}

	// Request contexts derive from ctx so long-lived SSE streams end on SIGTERM
	// instead of holding Shutdown until its timeout.
	srv := &http.Server{Addr: cfg.Listen, Handler: api.New(a), ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx }}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Info("http server listening", "addr", cfg.Listen)

	select {
	case <-ctx.Done():
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// dumpOpenAPI prints the OpenAPI document (used to generate web client types).
func dumpOpenAPI() error {
	dir, err := os.MkdirTemp("", "mangarr-openapi-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	os.Setenv("MANGARR_DATA_DIR", dir)
	os.Setenv("MANGARR_DB", "sqlite://"+dir+"/openapi.db")
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log, ring := logging.Setup("error", os.Stderr)
	a, err := app.New(context.Background(), cfg, log, ring)
	if err != nil {
		return err
	}
	defer a.Close()
	h := api.New(a)
	rec := &captureWriter{header: http.Header{}}
	req, _ := http.NewRequest(http.MethodGet, "/api/openapi.json", nil)
	req.Header.Set("X-Api-Key", mustAPIKey(a))
	h.ServeHTTP(rec, req)
	if rec.status != 0 && rec.status != 200 {
		return fmt.Errorf("openapi: status %d", rec.status)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, rec.body, "", "  "); err != nil {
		return err
	}
	out.WriteByte('\n')
	_, err = os.Stdout.Write(out.Bytes())
	return err
}

func mustAPIKey(a *app.App) string {
	g, err := a.Settings.General(context.Background())
	if err != nil {
		panic(err)
	}
	return g.APIKey
}

type captureWriter struct {
	header http.Header
	body   []byte
	status int
}

func (c *captureWriter) Header() http.Header { return c.header }
func (c *captureWriter) Write(b []byte) (int, error) {
	c.body = append(c.body, b...)
	return len(b), nil
}
func (c *captureWriter) WriteHeader(s int) { c.status = s }

// healthcheck is used by the Docker HEALTHCHECK (distroless has no curl).
func healthcheck() int {
	addr := os.Getenv("MANGARR_LISTEN")
	if addr == "" {
		addr = ":8787"
	}
	base := os.Getenv("MANGARR_URL_BASE")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://127.0.0.1" + addr + base + "/ping")
	if err != nil || resp.StatusCode != 200 {
		return 1
	}
	return 0
}

// bench runs `mangarr bench encode <chapter.cbz> [pages]`.
func bench(args []string) int {
	if len(args) < 2 || args[0] != "encode" {
		fmt.Fprintln(os.Stderr, "usage: mangarr bench encode <chapter.cbz> [max pages]")
		return 2
	}
	maxPages := 10
	if len(args) > 2 {
		if n, err := strconv.Atoi(args[2]); err == nil {
			maxPages = n
		}
	}
	enc := imageenc.Detect()
	formats := []string{"avif"}
	if _, ok := enc.Engine("jxl"); ok {
		formats = append(formats, "jxl")
	}
	if err := enc.Bench(context.Background(), args[1], formats, maxPages, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
