package api

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreWorkerOutputAssemblesChunks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.zip")
	parts := [][]byte{[]byte("first"), []byte("-second"), []byte("-third")}
	for i, part := range parts {
		if err := storeWorkerOutput(path, i+1, len(parts), part); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := bytes.Join(parts, nil); !bytes.Equal(got, want) {
		t.Fatalf("assembled %q, want %q", got, want)
	}
	if matches, _ := filepath.Glob(path + ".part*"); len(matches) != 0 {
		t.Fatalf("temporary chunks remain: %v", matches)
	}
	legacy := filepath.Join(dir, "legacy.zip")
	if err := storeWorkerOutput(legacy, 0, 0, []byte("old worker")); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(legacy); err != nil || string(got) != "old worker" {
		t.Fatalf("legacy output = %q, %v", got, err)
	}
}
