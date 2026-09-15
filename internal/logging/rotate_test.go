package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotating(t *testing.T) {
	dir := t.TempDir()
	r, err := OpenRotating(dir, "mangarr", 100, 3)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 39) + "\n" // 40 bytes
	for i := 0; i < 12; i++ {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	_ = r.Close()
	files := Files(dir)
	if len(files) != 3 {
		t.Fatalf("files = %+v", files)
	}
	for _, f := range files {
		if f.Size > 100 {
			t.Fatalf("%s is %d bytes", f.Name, f.Size)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "mangarr.3.txt")); err == nil {
		t.Fatal("kept more files than asked")
	}
	// reopening appends to the current file
	r, _ = OpenRotating(dir, "mangarr", 100, 3)
	_, _ = r.Write([]byte("y\n"))
	_ = r.Close()
	b, _ := os.ReadFile(filepath.Join(dir, "mangarr.txt"))
	if !strings.HasSuffix(string(b), "y\n") || !strings.HasPrefix(string(b), "x") {
		t.Fatalf("current file = %q", b)
	}
}
