package imagecheck

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func encode(t *testing.T, format string, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(1, 1, color.White)
	var buf bytes.Buffer
	var err error
	if format == "png" {
		err = png.Encode(&buf, img)
	} else {
		err = jpeg.Encode(&buf, img, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDetect(t *testing.T) {
	info, err := Detect(encode(t, "png", 40, 60))
	if err != nil || info.Format != "png" || info.Width != 40 || info.Height != 60 {
		t.Fatalf("png: %+v %v", info, err)
	}
	info, err = Detect(encode(t, "jpeg", 30, 20))
	if err != nil || info.Format != "jpeg" || info.Width != 30 {
		t.Fatalf("jpeg: %+v %v", info, err)
	}
	if _, err := Detect([]byte("  <!DOCTYPE html><html><body>Cloudflare</body></html>")); err != ErrHTML {
		t.Fatalf("html: %v", err)
	}
	if _, err := Detect(nil); err != ErrEmpty {
		t.Fatalf("empty: %v", err)
	}
	truncated := encode(t, "png", 40, 60)[:20]
	if _, err := Detect(truncated); err == nil {
		t.Fatal("expected error for truncated png")
	}
}
