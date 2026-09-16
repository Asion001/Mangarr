package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// TestWorkerKeys: a worker key is issued once, opens the worker endpoints
// and nothing else, and stops working when the worker is switched off.
func TestWorkerKeys(t *testing.T) {
	srv, a := newServer(t, false)
	g, _ := a.Settings.General(context.Background())
	admin := g.APIKey
	do := func(method, path, body, key string) *http.Response {
		var r *http.Request
		if body == "" {
			r, _ = http.NewRequest(method, srv.URL+path, nil)
		} else {
			r, _ = http.NewRequest(method, srv.URL+path, strings.NewReader(body))
			r.Header.Set("Content-Type", "application/json")
		}
		if key != "" {
			r.Header.Set("X-Api-Key", key)
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := do(http.MethodPost, "/api/v1/workers", `{"name":"gpu-box","roles":["upscale","download","nonsense"]}`, admin)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", resp.StatusCode)
	}
	var made struct {
		Worker struct {
			ID    int64    `json:"id"`
			Name  string   `json:"name"`
			Roles []string `json:"roles"`
		} `json:"worker"`
		Key string `json:"key"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&made); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(made.Key, "mgw_") || len(made.Worker.Roles) != 2 {
		t.Fatalf("new worker: %+v", made)
	}

	// the key is not an account: it opens nothing outside /api/v1/worker/
	if resp := do(http.MethodGet, "/api/v1/series", "", made.Key); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a worker key read the library: %d", resp.StatusCode)
	}
	if resp := do(http.MethodGet, "/api/v1/workers", "", made.Key); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a worker key listed the workers: %d", resp.StatusCode)
	}
	// and an unknown key is refused outright
	if resp := do(http.MethodGet, "/api/v1/series", "", "mgw_nope"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("an unknown worker key: %d", resp.StatusCode)
	}

	// the same name twice is a conflict
	if resp := do(http.MethodPost, "/api/v1/workers", `{"name":"gpu-box","roles":["upscale"]}`, admin); resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate name: %d", resp.StatusCode)
	}
	// a worker needs a role
	if resp := do(http.MethodPost, "/api/v1/workers", `{"name":"idle","roles":[]}`, admin); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("no roles: %d", resp.StatusCode)
	}

	// the listing shows it, without the key
	resp = do(http.MethodGet, "/api/v1/workers", "", admin)
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	// it counts as online: it has been here, even though it was turned away
	if len(list) != 1 || list[0]["online"] != true || list[0]["prefix"] == "" {
		t.Fatalf("listing: %+v", list)
	}
	if _, leaked := list[0]["keyHash"]; leaked {
		t.Fatal("the listing carries the key hash")
	}

	path := "/api/v1/workers/" + strconv.FormatInt(made.Worker.ID, 10)
	if resp := do(http.MethodPut, path, `{"enabled":false,"roles":["encode"]}`, admin); resp.StatusCode != 200 {
		t.Fatalf("update: %d", resp.StatusCode)
	}
	// a switched-off worker's key no longer authenticates at all
	if resp := do(http.MethodGet, "/api/v1/series", "", made.Key); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a disabled worker's key: %d", resp.StatusCode)
	}
	if resp := do(http.MethodDelete, path, "", admin); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp := do(http.MethodDelete, path, "", admin); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("delete twice: %d", resp.StatusCode)
	}
}
