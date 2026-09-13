package upscaler

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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

func zipOf(t *testing.T, files map[string][]byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, d := range files {
		w, _ := zw.Create(n)
		_, _ = w.Write(d)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
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

func TestUpscaleEndpoint(t *testing.T) {
	s := NewServer(Config{Token: "tok", TmpDir: t.TempDir(), CWebP: "-", Version: "test"}, fakeRunner{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	s.cfg.CWebP = "" // force JPEG fallback when webp is requested
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/info", nil)
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 401 {
		t.Fatalf("expected 401 without token, got %d", resp.StatusCode)
	}

	body := zipOf(t, map[string][]byte{"0001.jpg": jpegPage(300, 450), "0002.jpg": jpegPage(400, 600)})
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/upscale?model=waifu2x-cunet&scale=4&format=webp&maxWidth=1400", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d: %s", resp.StatusCode, out)
	}
	zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]image.Config{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		cfg, _, err := image.DecodeConfig(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		got[f.Name] = cfg
	}
	// 300*4 = 1200 (under cap), 400*4 = 1600 -> capped to 1400
	if got["0001.jpg"].Width != 1200 || got["0002.jpg"].Width != 1400 || got["0002.jpg"].Height != 2100 {
		t.Fatalf("unexpected outputs: %+v", got)
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/upscale?model=realcugan&scale=2", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 400 {
		t.Fatalf("unavailable model should be rejected, got %d", resp.StatusCode)
	}
}
