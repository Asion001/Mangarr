// Command mangarr-upscaler runs a processing node, the same as
// `mangarr` with MANGARR_MODE=upscaler. It exists for builds without the
// server (e.g. a Windows exe next to the ncnn binaries on a gaming PC).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Asion001/mangarr/internal/upscaler"
)

func main() {
	cfg, err := upscaler.LoadNodeConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		resp, err := http.Get("http://127.0.0.1" + cfg.Listen + "/healthz")
		if err != nil || resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := upscaler.RunNode(ctx, cfg, slog.New(slog.NewTextHandler(os.Stdout, nil))); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
