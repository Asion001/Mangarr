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

	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/imagecheck"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/upscale"
)

type Processor struct {
	mods *modules.Manager
}

func New(m *modules.Manager) *Processor { return &Processor{mods: m} }

func (p *Processor) upscaler(cfg model.UpscaleConfig) (upscale.Module, error) {
	if cfg.UpscalerID > 0 {
		m, _, err := modules.GetAs[upscale.Module](p.mods, cfg.UpscalerID)
		return m, err
	}
	list := modules.ActiveAs[upscale.Module](p.mods, modules.KindUpscale)
	if len(list) == 0 {
		return nil, errors.New("no upscaler module is configured")
	}
	return list[0].Instance, nil
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
	up, err := p.upscaler(cfg)
	if err != nil {
		return nil, false, "", err
	}
	info, err := up.Info(ctx)
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
	for scale, idxs := range groups {
		imgs := make([]upscale.Image, 0, len(idxs))
		for _, i := range idxs {
			data, err := os.ReadFile(pages[i].Path)
			if err != nil {
				return nil, false, "", err
			}
			imgs = append(imgs, upscale.Image{Name: pages[i].Name, Data: data})
		}
		res, err := up.Upscale(ctx, imgs, upscale.Params{Model: mdl.Name, Scale: scale, Noise: cfg.Noise, Format: format,
			Quality: cfg.Quality, MaxWidth: cfg.MaxWidth})
		if err != nil {
			return nil, false, "", err
		}
		if len(res) != len(idxs) {
			return nil, false, "", fmt.Errorf("upscaler returned %d of %d pages", len(res), len(idxs))
		}
		for k, i := range idxs {
			info, err := imagecheck.Detect(res[k].Data)
			if err != nil {
				return nil, false, "", fmt.Errorf("upscaled %s: %w", pages[i].Name, err)
			}
			base := strings.TrimSuffix(pages[i].Name, filepath.Ext(pages[i].Name))
			name := base + imagecheck.Ext(info.Format)
			path := filepath.Join(outDir, name)
			if err := os.WriteFile(path, res[k].Data, 0o664); err != nil {
				return nil, false, "", err
			}
			out[i] = downloads.PageFile{Name: name, Path: path, Format: info.Format, Width: info.Width, Height: info.Height}
		}
	}
	return out, true, mdl.Name, nil
}

var _ downloads.Processor = (*Processor)(nil)
