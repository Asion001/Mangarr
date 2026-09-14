// Package imageenc re-encodes page images to save storage: lossy AVIF
// (avifenc from libavif, or a slower built-in WebAssembly encoder) and
// lossless JPEG XL recompression (cjxl). Each page is kept only when the new
// file is meaningfully smaller, black-and-white pages are encoded without
// color, and pages that are already AVIF/JXL (or animated GIFs) are left alone.
package imageenc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/Asion001/mangarr/internal/imagecheck"
	"github.com/Asion001/mangarr/internal/model"
)

// Options are resolved encoder parameters.
type Options struct {
	Format  string // avif or jxl
	Quality int    // AVIF 1-100
	Speed   int    // avifenc -s (0-10) / cjxl -e (1-9)
}

// Resolve applies the preset of cfg and its overrides.
func Resolve(cfg model.EncodeConfig) Options {
	o := Options{Format: cfg.Format}
	switch cfg.Format {
	case "jxl":
		o.Speed = map[string]int{"max": 9, "balanced": 7, "fast": 4}[cfg.Preset]
		if o.Speed == 0 {
			o.Speed = 7
		}
	default:
		q := map[string][2]int{"max": {48, 3}, "balanced": {55, 6}, "fast": {60, 8}}[cfg.Preset]
		if q == [2]int{} {
			q = [2]int{55, 6}
		}
		o.Quality, o.Speed = q[0], q[1]
	}
	if cfg.Quality > 0 {
		o.Quality = min(cfg.Quality, 100)
	}
	if cfg.Speed > 0 {
		o.Speed = cfg.Speed
	}
	return o
}

// Engine encodes one image file.
type Engine interface {
	Name() string
	// Format is the output format (avif or jxl).
	Format() string
	// Accepts reports whether the engine reads srcFormat directly.
	Accepts(srcFormat string) bool
	// Encode writes src (in srcFormat) to dst. gray requests a grayscale encode.
	Encode(ctx context.Context, src, srcFormat, dst string, o Options, gray bool) error
	// Slow marks engines that are much slower than native ones.
	Slow() bool
}

// Page is an image file on disk.
type Page struct {
	Name   string
	Path   string
	Format string
	Width  int
	Height int
}

// Stats summarizes an encode run.
type Stats struct {
	Engine  string `json:"engine"`
	Encoded int    `json:"encoded"`
	Kept    int    `json:"kept"`
	Skipped int    `json:"skipped"`
	Before  int64  `json:"before"`
	After   int64  `json:"after"`
}

// Encoder picks an engine per format and encodes pages in parallel.
type Encoder struct {
	engines []Engine
	// Threads is the number of pages encoded at once.
	Threads int
}

// New returns an encoder using the given engines (earlier = preferred).
func New(engines ...Engine) *Encoder {
	return &Encoder{engines: engines, Threads: max(runtime.NumCPU()-1, 1)}
}

// Detect finds the installed engines: avifenc and cjxl on PATH, plus the
// built-in AVIF encoder as a fallback.
func Detect() *Encoder {
	var list []Engine
	if e := FindAvifenc(); e != nil {
		list = append(list, e)
	}
	if e := FindCjxl(); e != nil {
		list = append(list, e)
	}
	list = append(list, WASMAvif{})
	return New(list...)
}

// Engine returns the preferred engine for format.
func (e *Encoder) Engine(format string) (Engine, bool) {
	for _, en := range e.engines {
		if en.Format() == format {
			return en, true
		}
	}
	return nil, false
}

// Engines lists the available engines.
func (e *Encoder) Engines() []Engine { return append([]Engine(nil), e.engines...) }

// ErrNoEngine means no engine can produce the requested format.
var ErrNoEngine = errors.New("no encoder for this format is installed")

// skip reports pages that are never re-encoded.
func skip(format, target string) bool {
	switch format {
	case "avif", "jxl", "gif", "":
		return true
	}
	// lossless JPEG XL only makes sense for JPEG (reversible) and PNG
	return target == "jxl" && format != "jpeg" && format != "png"
}

// EncodePages re-encodes pages into workDir and returns the pages to keep:
// the new file when it's at least minSavingsPct smaller, else the original.
func (e *Encoder) EncodePages(ctx context.Context, pages []Page, cfg model.EncodeConfig, workDir string) ([]Page, Stats, error) {
	var st Stats
	if cfg.Format == "" || cfg.Format == "keep" {
		return pages, st, nil
	}
	eng, ok := e.Engine(cfg.Format)
	if !ok {
		return nil, st, fmt.Errorf("%w (%s)", ErrNoEngine, cfg.Format)
	}
	st.Engine = eng.Name()
	o := Resolve(cfg)
	outDir := filepath.Join(workDir, "encoded")
	if err := os.MkdirAll(outDir, 0o775); err != nil {
		return nil, st, err
	}
	out := append([]Page(nil), pages...)
	var mu sync.Mutex
	var firstErr error
	sem := make(chan struct{}, max(e.Threads, 1))
	var wg sync.WaitGroup
	for i, p := range pages {
		if skip(p.Format, cfg.Format) {
			st.Skipped++
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			np, before, after, err := e.encodeOne(ctx, eng, p, cfg, o, outDir)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", p.Name, err)
				}
				return
			}
			st.Before += before
			if np != nil {
				out[i] = *np
				st.Encoded++
				st.After += after
			} else {
				st.Kept++
				st.After += before
			}
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, st, firstErr
	}
	return out, st, ctx.Err()
}

func (e *Encoder) encodeOne(ctx context.Context, eng Engine, p Page, cfg model.EncodeConfig, o Options, outDir string) (*Page, int64, int64, error) {
	fi, err := os.Stat(p.Path)
	if err != nil {
		return nil, 0, 0, err
	}
	before := fi.Size()
	src, srcFormat := p.Path, p.Format
	gray := false
	if cfg.Grayscale || !eng.Accepts(srcFormat) {
		img, err := decodeFile(p.Path)
		if err != nil {
			return nil, before, 0, err
		}
		gray = cfg.Grayscale && IsGrayscale(img)
		if !eng.Accepts(srcFormat) { // e.g. WebP into avifenc: go through PNG
			tmp := filepath.Join(outDir, strings.TrimSuffix(p.Name, filepath.Ext(p.Name))+".src.png")
			if err := writePNG(tmp, img); err != nil {
				return nil, before, 0, err
			}
			defer os.Remove(tmp)
			src, srcFormat = tmp, "png"
		}
	}
	base := strings.TrimSuffix(p.Name, filepath.Ext(p.Name))
	dst := filepath.Join(outDir, base+"."+eng.Format())
	if err := eng.Encode(ctx, src, srcFormat, dst, o, gray); err != nil {
		return nil, before, 0, err
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		return nil, before, 0, err
	}
	if info, err := imagecheck.Detect(data); err != nil || info.Format != eng.Format() {
		return nil, before, 0, fmt.Errorf("encoder produced an invalid %s file", eng.Format())
	}
	after := int64(len(data))
	if after >= before*int64(100-max(cfg.MinSavingsPct, 0))/100 {
		os.Remove(dst)
		return nil, before, after, nil // not worth it: keep the original
	}
	np := p
	np.Name, np.Path, np.Format = base+"."+eng.Format(), dst, eng.Format()
	return &np, before, after, nil
}

func decodeFile(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// IsGrayscale reports whether (nearly) every sampled pixel is gray. Scans
// with slightly tinted paper still count; color panels don't.
func IsGrayscale(img image.Image) bool {
	switch img.(type) {
	case *image.Gray, *image.Gray16:
		return true
	}
	b := img.Bounds()
	step := max(1, min(b.Dx(), b.Dy())/200)
	total, colored := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r, g, bl, _ := img.At(x, y).RGBA()
			r8, g8, b8 := int(r>>8), int(g>>8), int(bl>>8)
			if max(abs(r8-g8), abs(g8-b8), abs(r8-b8)) > 24 {
				colored++
			}
			total++
		}
	}
	return total > 0 && colored*1000 <= total*3 // at most 0.3% colored samples
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
