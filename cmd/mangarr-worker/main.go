// Command mangarr-worker runs a worker, the same as `mangarr` with
// MANGARR_MODE=worker. It exists for builds without the server (a Windows
// exe next to the ncnn binaries on a gaming PC, say): it asks a mangarr
// server for work and needs no inbound access of its own.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Asion001/mangarr/internal/upscaler"
	"github.com/Asion001/mangarr/internal/version"
	"github.com/Asion001/mangarr/internal/worker"
)

func main() {
	cfg, err := worker.LoadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck": // a worker listens on nothing: being up is being well
			return
		case "version":
			fmt.Println(version.Version, "build", version.Build, version.Commit)
			return
		}
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg.Log, cfg.Version = log, version.Version
	if engine, err := upscaler.LoadEngineConfig(os.Getenv); err == nil {
		cfg.Upscaler = upscaler.NewEngine(engine, log)
	}
	w, err := worker.New(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := w.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
