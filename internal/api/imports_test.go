package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/api"
	"github.com/Asion001/mangarr/internal/backupimport"
)

func TestImportUpload(t *testing.T) {
	srv, a := newServer(t, true)
	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	post := func(body []byte) *http.Response {
		resp, err := http.Post(srv.URL+"/api/v1/imports?fileName=my.tachibk", "application/octet-stream", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	if resp := post([]byte(`{"version":2,"mangas":[]}`)); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("legacy JSON: %d", resp.StatusCode)
	}
	b := &backupimport.Backup{Entries: []backupimport.BackupManga{{SourceID: "1", URL: "/m", Title: "Nowhere", Favorite: true}}}
	resp := post(backupimport.MarshalMihon(b))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload: %d", resp.StatusCode)
	}
	var imp api.ImportResource
	_ = json.NewDecoder(resp.Body).Decode(&imp)
	if imp.Format != backupimport.FormatMihon || imp.FileName != "my.tachibk" || imp.Info.Entries != 1 {
		t.Fatalf("import = %+v", imp)
	}
	// no source module: the entry needs review once matched
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(srv.URL + "/api/v1/imports/" + itoa(imp.ID) + "/entries")
		if err != nil {
			t.Fatal(err)
		}
		var page api.ImportEntriesPage
		_ = json.NewDecoder(resp.Body).Decode(&page)
		if len(page.Items) == 1 && page.Items[0].State == "review" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("entry wasn't matched")
}
