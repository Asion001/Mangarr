package cbz

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteReadAtomic(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "Series Ch.0001.cbz")
	pagePath := filepath.Join(dir, "p2.png")
	if err := os.WriteFile(pagePath, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	pages := []Page{{Name: "0002.png", Path: pagePath}, {Name: "0001.jpg", Data: []byte("first")}}
	res, err := Write(dst, pages, []byte("<ComicInfo/>"), 0o640, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if res.Size == 0 || len(res.SHA256) != 64 {
		t.Fatalf("bad result %+v", res)
	}
	if _, err := os.Stat(dst + PartialSuffix); !os.IsNotExist(err) {
		t.Fatal("partial file left behind")
	}
	st, _ := os.Stat(dst)
	if st.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", st.Mode())
	}
	got, ci, err := Read(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(ci) != "<ComicInfo/>" || len(got) != 2 || got[0].Name != "0001.jpg" || string(got[1].Data) != "second" {
		t.Fatalf("read mismatch: %+v %s", got, ci)
	}

	// Overwrite keeps the same path (upgrade) and is still atomic.
	if _, err := Write(dst, []Page{{Name: "0001.jpg", Data: []byte("new")}}, nil, 0, time.Time{}); err != nil {
		t.Fatal(err)
	}
	got, ci, _ = Read(dst)
	if len(got) != 1 || ci != nil || string(got[0].Data) != "new" {
		t.Fatal("overwrite failed")
	}
}

func TestWriteFailureCleansUp(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "x.cbz")
	_, err := Write(dst, []Page{{Name: "0001.jpg", Path: filepath.Join(dir, "missing.jpg")}}, nil, 0, time.Time{})
	if err == nil {
		t.Fatal("expected error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}
}
