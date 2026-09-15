// Package fakeupscaler provides a GPU-free upscaler engine for tests: it
// scales with nearest-neighbour and pretends to be "waifu2x-cunet".
package fakeupscaler

import (
	"context"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"golang.org/x/image/draw"

	"github.com/Asion001/mangarr/internal/upscaler"
)

// Runner scales images with nearest-neighbour instead of a GPU model.
type Runner struct{}

func (Runner) Available(e upscaler.Engine) bool { return e.Name == "waifu2x-cunet" }

// MaxBatch is the most pages seen in one run.
var MaxBatch atomic.Int64

func (Runner) Run(ctx context.Context, e upscaler.Engine, in, out string, scale, noise int) error {
	entries, err := os.ReadDir(in)
	if err != nil {
		return err
	}
	for n := int64(len(entries)); ; {
		cur := MaxBatch.Load()
		if n <= cur || MaxBatch.CompareAndSwap(cur, n) {
			break
		}
	}
	for _, ent := range entries {
		f, err := os.Open(filepath.Join(in, ent.Name()))
		if err != nil {
			return err
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			return err
		}
		b := img.Bounds()
		dst := image.NewRGBA(image.Rect(0, 0, b.Dx()*scale, b.Dy()*scale))
		draw.NearestNeighbor.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		o, err := os.Create(filepath.Join(out, strings.TrimSuffix(ent.Name(), filepath.Ext(ent.Name()))+".png"))
		if err != nil {
			return err
		}
		if err := png.Encode(o, dst); err != nil {
			o.Close()
			return err
		}
		o.Close()
	}
	return nil
}
