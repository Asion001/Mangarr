package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/Asion001/mangarr/internal/backup"
)

func TestBackupUploadStreamsAndVerifies(t *testing.T) {
	srv, a := newServer(t, true)
	b, err := a.Backups.Create(context.Background(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	path, err := a.Backups.Path(b.Name)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(srv.URL+"/api/v1/system/backups/upload", "application/zip", bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status %d", resp.StatusCode)
	}
	var uploaded backup.Backup
	if err := json.NewDecoder(resp.Body).Decode(&uploaded); err != nil {
		t.Fatal(err)
	}
	if !uploaded.Restorable || uploaded.Verification == nil || uploaded.Verification.Checksum == "" {
		t.Fatalf("upload response lacks verification: %+v", uploaded)
	}
	verify, err := http.Post(srv.URL+"/api/v1/system/backups/"+uploaded.Name+"/verify", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer verify.Body.Close()
	if verify.StatusCode != http.StatusOK {
		t.Fatalf("verify status %d", verify.StatusCode)
	}
}
