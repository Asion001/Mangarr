package imageenc

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Asion001/mangarr/internal/imagecheck"
	"github.com/Asion001/mangarr/internal/model"
)

func TestResolve(t *testing.T) {
	if o := Resolve(model.EncodeConfig{Format: "avif", Preset: "max"}); o.Quality != 48 || o.Speed != 3 {
		t.Fatalf("max: %+v", o)
	}
	if o := Resolve(model.EncodeConfig{Format: "avif", Preset: "fast", Quality: 70, Speed: 9}); o.Quality != 70 || o.Speed != 9 {
		t.Fatalf("overrides: %+v", o)
	}
	if o := Resolve(model.EncodeConfig{Format: "jxl", Preset: "balanced"}); o.Speed != 7 {
		t.Fatalf("jxl: %+v", o)
	}
}

func grayImg() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 200, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 200; x++ {
			v := uint8((x + y) % 250)
			img.Set(x, y, color.RGBA{v, v, v + 3, 255}) // slightly tinted paper still counts as gray
		}
	}
	return img
}

func TestIsGrayscale(t *testing.T) {
	g := grayImg()
	if !IsGrayscale(g) {
		t.Fatal("tinted gray scan should be grayscale")
	}
	for y := 50; y < 150; y++ {
		for x := 50; x < 150; x++ {
			g.Set(x, y, color.RGBA{220, 30, 30, 255})
		}
	}
	if IsGrayscale(g) {
		t.Fatal("a red panel is color")
	}
}

// fakeEngine "encodes" by writing a file of a chosen size.
type fakeEngine struct {
	mu      sync.Mutex
	accepts []string
	outSize int // bytes written per page
	calls   []string
	gray    []bool
}

func (f *fakeEngine) Name() string          { return "fake" }
func (f *fakeEngine) Format() string        { return "avif" }
func (f *fakeEngine) Slow() bool            { return false }
func (f *fakeEngine) Accepts(s string) bool { return slices.Contains(f.accepts, s) }
func (f *fakeEngine) Encode(_ context.Context, src, srcFormat, dst string, _ Options, gray bool) error {
	f.mu.Lock()
	f.calls = append(f.calls, srcFormat)
	f.gray = append(f.gray, gray)
	f.mu.Unlock()
	// a valid AVIF header followed by padding
	hdr, _ := os.ReadFile("../imagecheck/testdata/gray.avif")
	return os.WriteFile(dst, append(hdr, make([]byte, max(f.outSize-len(hdr), 0))...), 0o644)
}

func writePage(t *testing.T, dir, name string, img image.Image, format string) Page {
	t.Helper()
	var buf bytes.Buffer
	switch format {
	case "jpeg":
		_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95})
	case "png":
		_ = png.Encode(&buf, img)
	default:
		buf.WriteString("GIF89a....")
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return Page{Name: name, Path: p, Format: format, Width: 200, Height: 300}
}

func TestEncodePages(t *testing.T) {
	dir := t.TempDir()
	colored := image.NewRGBA(image.Rect(0, 0, 200, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 200; x++ {
			colored.Set(x, y, color.RGBA{uint8(x), 40, uint8(255 - y%256), 255})
		}
	}
	pages := []Page{
		writePage(t, dir, "0001.jpg", grayImg(), "jpeg"),
		writePage(t, dir, "0002.png", colored, "png"),
		writePage(t, dir, "0003.gif", nil, "gif"),
		{Name: "0004.avif", Path: "x", Format: "avif"},
	}
	eng := &fakeEngine{accepts: []string{"jpeg"}, outSize: 600}
	enc := New(eng)
	out, st, err := enc.EncodePages(context.Background(), pages, model.EncodeConfig{Format: "avif", Preset: "balanced", Grayscale: true, MinSavingsPct: 10}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Encoded != 2 || st.Skipped != 2 || out[0].Format != "avif" || out[0].Name != "0001.avif" || out[2].Format != "gif" || out[3].Format != "avif" {
		t.Fatalf("stats %+v out %+v", st, out)
	}
	if out[0].Width != 200 {
		t.Fatal("dimensions are kept")
	}
	// the PNG went through Go (engine doesn't accept it) and only the gray page is gray
	slices.Sort(eng.calls)
	if !slices.Equal(eng.calls, []string{"jpeg", "png"}) {
		t.Fatalf("calls %v", eng.calls)
	}
	grays := 0
	for _, g := range eng.gray {
		if g {
			grays++
		}
	}
	if grays != 1 {
		t.Fatalf("want exactly one grayscale encode, got %v", eng.gray)
	}

	// too little savings: keep the originals
	eng2 := &fakeEngine{accepts: []string{"jpeg", "png"}, outSize: 1 << 20}
	out, st, err = New(eng2).EncodePages(context.Background(), pages[:2], model.EncodeConfig{Format: "avif", MinSavingsPct: 10}, t.TempDir())
	if err != nil || st.Kept != 2 || out[0].Format != "jpeg" || out[1].Format != "png" {
		t.Fatalf("keep originals: %+v %+v %v", st, out, err)
	}

	// no engine for the format
	if _, _, err := New(eng).EncodePages(context.Background(), pages, model.EncodeConfig{Format: "jxl"}, dir); !errors.Is(err, ErrNoEngine) {
		t.Fatalf("want ErrNoEngine, got %v", err)
	}
}

func TestAvifencArgsAndTuneFallback(t *testing.T) {
	var calls [][]string
	orig := run
	defer func() { run = orig }()
	run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		all := append([]string{name}, args...)
		calls = append(calls, all)
		if slices.Contains(args, "tune=iq") {
			return []byte("Invalid codec-specific option: tune=iq"), errors.New("exit status 1")
		}
		return nil, nil
	}
	a := &Avifenc{Bin: "/usr/bin/avifenc"}
	if err := a.Encode(context.Background(), "in.png", "png", "out.avif", Options{Quality: 55, Speed: 6}, true); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("want a retry without tune, got %v", calls)
	}
	got := strings.Join(calls[1], " ")
	for _, want := range []string{"-j 1", "-s 6", "-q 55", "-d 8", "-y 400", "in.png out.avif"} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", got, want)
		}
	}
	calls = nil
	_ = a.Encode(context.Background(), "in.png", "png", "out.avif", Options{Quality: 55, Speed: 6}, false)
	if len(calls) != 1 || !strings.Contains(strings.Join(calls[0], " "), "-y 420") {
		t.Fatalf("tune=iq should not be retried once unsupported: %v", calls)
	}
}

func TestWASMAvif(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	dir := t.TempDir()
	p := writePage(t, dir, "0001.png", grayImg(), "png")
	dst := filepath.Join(dir, "0001.avif")
	if err := (WASMAvif{}).Encode(context.Background(), p.Path, "png", dst, Options{Quality: 55, Speed: 8}, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(dst)
	info, err := imagecheck.Detect(data)
	if err != nil || info.Format != "avif" || info.Width != 200 || info.Height != 300 {
		t.Fatalf("wasm output: %+v %v", info, err)
	}
}
