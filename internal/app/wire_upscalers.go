package app

import (
	"context"
	"strings"

	"github.com/Asion001/mangarr/internal/config"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/upscaler"
)

// wireUpscalers offers the built-in upscaler when this image has the tools
// for it. Upscaling on another machine is a worker with the upscale role
// (System → Workers), which brings its own module with it.
func (a *App) wireUpscalers(ctx context.Context) error {
	if a.Cfg.Mode == config.ModeIntegrated {
		return a.offerLocalUpscaler(ctx)
	}
	return nil
}

// localOfferedKey remembers that the built-in upscaler was set up once (so
// deleting it in the UI sticks).
const localOfferedKey = "local_upscaler_offered"

// offerLocalUpscaler adds the built-in upscaler when the image has the tools.
func (a *App) offerLocalUpscaler(ctx context.Context) error {
	var offered bool
	_ = a.Settings.Get(ctx, localOfferedKey, &offered)
	dir := "/opt/upscalers"
	if offered || !upscaler.ToolsAvailable(dir) {
		return nil
	}
	// only enabled by default with a real GPU (lavapipe on the CPU is very slow)
	gpu := false
	for _, d := range upscaler.Devices() {
		if !strings.Contains(strings.ToLower(d), "llvmpipe") && !strings.Contains(strings.ToLower(d), "lavapipe") {
			gpu = true
		}
	}
	def := &model.ProviderDefinition{Kind: string(modules.KindUpscale), Implementation: "local", Name: "Built-in (this server)",
		Enabled: gpu, Priority: 50, Settings: map[string]any{"toolsDir": dir, "gpu": "auto"}}
	if err := a.Modules.Create(ctx, def); err != nil {
		return err
	}
	a.Log.Info("added the built-in upscaler", "gpu", gpu)
	return a.Settings.Set(ctx, localOfferedKey, true)
}

// OfferWorkersUpscaler adds the module that upscales on the workers, the
// first time a worker with that role dials in — so a GPU box only has to be
// given a key.
func (a *App) OfferWorkersUpscaler(ctx context.Context) {
	n, err := a.DB.NewSelect().Model((*model.ProviderDefinition)(nil)).
		Where("kind = ? AND implementation = ?", string(modules.KindUpscale), "workers").Count(ctx)
	if err != nil || n > 0 {
		return
	}
	def := &model.ProviderDefinition{Kind: string(modules.KindUpscale), Implementation: "workers", Name: "Workers",
		Enabled: true, Priority: 20, Settings: map[string]any{}}
	if err := a.Modules.Create(ctx, def); err != nil {
		a.Log.Warn("could not add the workers upscaler", "err", err)
		return
	}
	a.Log.Info("added the upscaler that hands batches to the workers")
	a.PushProcessBacklog("worker-online") // chapters that were waiting for one
}
