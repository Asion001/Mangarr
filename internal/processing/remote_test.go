package processing

import (
	"path/filepath"
	"testing"
)

func TestInsideRefusesEscapes(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"", ".", "..", "../x.png", "a/../../x.png", "/etc/passwd"} {
		if _, err := inside(dir, bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	got, err := inside(dir, "upscaled/0001.png")
	if err != nil || got != filepath.Join(dir, "upscaled", "0001.png") {
		t.Fatalf("got %q, %v", got, err)
	}
}
