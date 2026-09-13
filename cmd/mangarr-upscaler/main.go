// Command mangarr-upscaler is the page upscaling worker used by mangarr's
// "ncnn-worker" upscale module. It wraps waifu2x/Real-CUGAN/Real-ESRGAN
// ncnn-vulkan binaries and runs one batch at a time per GPU.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Asion001/mangarr/internal/upscaler"
	"github.com/Asion001/mangarr/internal/version"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		resp, err := http.Get("http://127.0.0.1" + env("UPSCALER_LISTEN", ":8788") + "/healthz")
		if err != nil || resp.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	tile, _ := strconv.Atoi(env("UPSCALER_TILE", "0"))
	runner := upscaler.CLIRunner{
		ToolsDir: env("UPSCALER_TOOLS_DIR", "/opt/upscalers"),
		GPU:      env("UPSCALER_GPU", "auto"),
		Threads:  env("UPSCALER_THREADS", ""),
		Tile:     tile,
	}
	timeout, err := time.ParseDuration(env("UPSCALER_TIMEOUT", "30m"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "UPSCALER_TIMEOUT:", err)
		os.Exit(1)
	}
	srv := upscaler.NewServer(upscaler.Config{
		Token:   os.Getenv("UPSCALER_TOKEN"),
		TmpDir:  env("UPSCALER_TMP_DIR", os.TempDir()),
		CWebP:   os.Getenv("UPSCALER_CWEBP"),
		Timeout: timeout,
		Version: version.Version,
	}, runner, log)
	info := srv.Info()
	names := []string{}
	for _, m := range info.Models {
		names = append(names, m.Name)
	}
	log.Info("mangarr-upscaler starting", "version", version.Version, "models", names, "devices", info.Devices, "formats", info.Formats)
	if len(names) == 0 {
		log.Warn("no upscaler tools found; set UPSCALER_TOOLS_DIR", "dir", runner.ToolsDir)
	}
	if os.Getenv("UPSCALER_TOKEN") == "" {
		log.Warn("UPSCALER_TOKEN is not set; anyone who can reach this port can use the GPU")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	hs := &http.Server{Addr: env("UPSCALER_LISTEN", ":8788"), Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
