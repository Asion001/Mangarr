package upscaler

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/draw"
)

// fakeRunner scales images with nearest-neighbour instead of a GPU model.
type fakeRunner struct{}

func (fakeRunner) Available(e Engine) bool { return e.Name == "waifu2x-cunet" }

func (fakeRunner) Run(ctx context.Context, e Engine, in, out string, scale, noise int) error {
	entries, err := os.ReadDir(in)
	if err != nil {
		return err
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

func jpegPage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, x%h, color.White)
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

// TestProcess: a batch is upscaled by the engine, capped at the requested
// width, and an engine this machine doesn't have is refused.
func TestProcess(t *testing.T) {
	s := NewServer(Config{TmpDir: t.TempDir(), CWebP: "-", Version: "test"}, fakeRunner{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.cfg.CWebP = "" // force JPEG fallback when webp is requested
	in := []Image{{Name: "0001.jpg", Data: jpegPage(300, 450)}, {Name: "0002.jpg", Data: jpegPage(400, 600)}}

	out, err := s.Process(context.Background(), Params{Model: "waifu2x-cunet", Scale: 4, Format: "webp", MaxWidth: 1400}, in)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]image.Config{}
	for _, img := range out {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
		if err != nil {
			t.Fatal(err)
		}
		got[img.Name] = cfg
	}
	// 300*4 = 1200 (under cap), 400*4 = 1600 -> capped to 1400
	if got["0001.jpg"].Width != 1200 || got["0002.jpg"].Width != 1400 || got["0002.jpg"].Height != 2100 {
		t.Fatalf("unexpected outputs: %+v", got)
	}

	if _, err := s.Process(context.Background(), Params{Model: "realcugan", Scale: 2}, in); err == nil {
		t.Fatal("a model this machine doesn't have should be refused")
	}
}
