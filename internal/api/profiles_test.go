package api_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Asion001/mangarr/internal/model"
)

func doJSON(t *testing.T, method, url, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestSingleDefaultProfile(t *testing.T) {
	srv, _ := newServer(t, true)
	var list []model.Profile
	doJSON(t, http.MethodGet, srv.URL+"/api/v1/profiles", "", &list)
	p := list[0]
	p.ID, p.Name, p.IsDefault = 0, "Webtoons", true
	body, _ := json.Marshal(p)
	var created model.Profile
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/v1/profiles", string(body), &created); code != 200 {
		t.Fatalf("create: %d", code)
	}
	list = nil
	doJSON(t, http.MethodGet, srv.URL+"/api/v1/profiles", "", &list)
	defaults := 0
	for _, p := range list {
		if p.IsDefault {
			defaults++
			if p.ID != created.ID {
				t.Fatalf("old profile %q still default", p.Name)
			}
		}
	}
	if len(list) != 2 || defaults != 1 {
		t.Fatalf("want 2 profiles with 1 default, got %+v", list)
	}
	// the default can't be un-defaulted without choosing another one
	created.IsDefault = false
	body, _ = json.Marshal(created)
	if code := doJSON(t, http.MethodPut, srv.URL+"/api/v1/profiles/"+itoa(created.ID), string(body), nil); code != 400 {
		t.Fatalf("un-default: %d", code)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
