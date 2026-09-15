// Package upscaling implements the download pipeline's processing stage:
// it upscales pages narrower than the profile's minimum width through an
// upscale module and keeps every other page untouched.
package upscaling

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/imagecheck"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/upscale"
	"github.com/Asion001/mangarr/internal/progress"
)

type Processor struct {
	mods *modules.Manager
	// Online (optional) reports whether an upscaler instance is reachable
	// (e.g. a desktop GPU node that may be switched off).
	Online func(def model.ProviderDefinition) bool
}

func New(m *modules.Manager) *Processor { return &Processor{mods: m} }

// upscaler returns the configured upscaler, or the first reachable one by priority.
func (p *Processor) upscaler(ctx context.Context, cfg model.UpscaleConfig) (upscale.Module, *upscale.Info, error) {
	if cfg.UpscalerID > 0 {
		m, _, err := modules.GetAs[upscale.Module](p.mods, cfg.UpscalerID)
		if err != nil {
			return nil, nil, err
		}
		info, err := m.Info(ctx)
		return m, info, err
	}
	list := modules.ActiveAs[upscale.Module](p.mods, modules.KindUpscale)
	if len(list) == 0 {
		return nil, nil, errors.New("no upscaler module is configured")
	}
	var errs []string
	for _, t := range list {
		if p.Online != nil && !p.Online(t.Def) {
			errs = append(errs, t.Def.Name+": offline")
			continue
		}
		ictx, cancel := context.WithTimeout(ctx, 20*time.Second)
		info, err := t.Instance.Info(ictx)
		cancel()
		if err == nil && len(info.Models) > 0 {
			return t.Instance, info, nil
		}
		if err == nil {
			err = errors.New("no models")
		}
		errs = append(errs, t.Def.Name+": "+err.Error())
	}
	return nil, nil, fmt.Errorf("no upscaler available (%s)", strings.Join(errs, "; "))
}

// ChooseScale picks the smallest supported scale that brings width to at
// least minWidth (or the largest scale when none does).
func ChooseScale(width, minWidth int, scales []int) int {
	s := append([]int(nil), scales...)
	sort.Ints(s)
	for _, x := range s {
		if x >= 2 && width*x >= minWidth {
			return x
		}
	}
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] >= 2 {
			return s[i]
		}
	}
	return 0
}

// NeedsUpscale reports whether a page should be upscaled.
func NeedsUpscale(pg downloads.PageFile, minWidth int) bool {
	if minWidth <= 0 || pg.Width <= 0 || pg.Width >= minWidth {
		return false
	}
	switch pg.Format {
	case "jpeg", "png", "webp", "bmp":
		return true
	}
	return false // gif (animations), avif, jxl are left alone
}

func (p *Processor) Process(ctx context.Context, cfg model.UpscaleConfig, pages []downloads.PageFile, workDir string) ([]downloads.PageFile, bool, string, error) {
	var todo []int
	for i, pg := range pages {
		if NeedsUpscale(pg, cfg.MinWidth) {
			todo = append(todo, i)
		}
	}
	if len(todo) == 0 {
		return pages, false, "", nil
	}
	up, info, err := p.upscaler(ctx, cfg)
	if err != nil {
		return nil, false, "", err
	}
	var mdl *upscale.Model
	for i := range info.Models {
		if info.Models[i].Name == cfg.Model {
			mdl = &info.Models[i]
		}
	}
	if mdl == nil {
		if len(info.Models) == 0 {
			return nil, false, "", errors.New("upscaler has no models")
		}
		mdl = &info.Models[0]
	}
	format := cfg.Format
	if format == "" {
		format = "webp"
	}
	// group pages by the scale they need so each batch is one engine run
	groups := map[int][]int{}
	for _, i := range todo {
		s := ChooseScale(pages[i].Width, cfg.MinWidth, mdl.Scales)
		if s == 0 {
			continue
		}
		groups[s] = append(groups[s], i)
	}
	out := append([]downloads.PageFile(nil), pages...)
	outDir := filepath.Join(workDir, "upscaled")
	if err := os.MkdirAll(outDir, 0o775); err != nil {
		return nil, false, "", err
	}
	done, total := 0, 0
	for _, idxs := range groups {
		total += len(idxs)
	}
	progress.Report(ctx, progress.Event{Stage: progress.StageUpscale, Total: total})
	for scale, group := range groups {
		for start := 0; start < len(group); start += ChunkPages {
			idxs := group[start:min(start+ChunkPages, len(group))]
			if err := p.upscaleChunk(ctx, up, mdl, cfg, format, scale, pages, idxs, out, outDir); err != nil {
				return nil, false, "", err
			}
			done += len(idxs)
			progress.Report(ctx, progress.Event{Stage: progress.StageUpscale, Done: done, Total: total})
		}
	}
	return out, true, mdl.Name, nil
}

// ChunkPages is how many pages go to the upscaler at once: short runs keep
// memory bounded, stay far from the upscaler's time limit on slow GPUs and
// show progress.
var ChunkPages = 8

func (p *Processor) upscaleChunk(ctx context.Context, up upscale.Module, mdl *upscale.Model, cfg model.UpscaleConfig, format string, scale int,
	pages []downloads.PageFile, idxs []int, out []downloads.PageFile, outDir string) error {
	imgs := make([]upscale.Image, 0, len(idxs))
	for _, i := range idxs {
		data, err := os.ReadFile(pages[i].Path)
		if err != nil {
			return err
		}
		imgs = append(imgs, upscale.Image{Name: pages[i].Name, Data: data})
	}
	res, err := up.Upscale(ctx, imgs, upscale.Params{Model: mdl.Name, Scale: scale, Noise: cfg.Noise, Format: format,
		Quality: cfg.Quality, MaxWidth: cfg.MaxWidth})
	if err != nil {
		return err
	}
	if len(res) != len(idxs) {
		return fmt.Errorf("upscaler returned %d of %d pages", len(res), len(idxs))
	}
	for k, i := range idxs {
		info, err := imagecheck.Detect(res[k].Data)
		if err != nil {
			return fmt.Errorf("upscaled %s: %w", pages[i].Name, err)
		}
		base := strings.TrimSuffix(pages[i].Name, filepath.Ext(pages[i].Name))
		name := base + imagecheck.Ext(info.Format)
		path := filepath.Join(outDir, name)
		if err := os.WriteFile(path, res[k].Data, 0o664); err != nil {
			return err
		}
		out[i] = downloads.PageFile{Name: name, Path: path, Format: info.Format, Width: info.Width, Height: info.Height}
	}
	return nil
}
