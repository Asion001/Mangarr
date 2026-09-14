package diskcache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrim(t *testing.T) {
	root := t.TempDir()
	mk := func(name string, size int, age time.Duration) {
		p := filepath.Join(root, "thumbs", name)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, make([]byte, size), 0o644)
		_ = os.Chtimes(p, time.Now().Add(-age), time.Now().Add(-age))
	}
	mk("old", 100, 40*24*time.Hour)
	mk("a", 100, 3*time.Hour)
	mk("b", 100, 2*time.Hour)
	mk("c", 100, time.Hour)
	if n := Trim(root, 30*24*time.Hour, 250); n != 2 {
		t.Fatalf("removed %d, want old + a", n)
	}
	st := Stats(root)
	if st[0].Files != 2 || st[0].Bytes != 200 {
		t.Fatalf("stats %+v", st)
	}
	if err := Clear(root, []string{"thumbs"}); err != nil || Stats(root)[0].Files != 0 {
		t.Fatal("clear")
	}
}
