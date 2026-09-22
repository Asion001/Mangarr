package api_test

import (
	"net/http"
	"testing"
)

// With no metadata module switched on, lookup says so instead of looking
// like the title wasn't found.
func TestLookupReportsProviders(t *testing.T) {
	srv, _ := newServer(t, true)
	var res struct {
		Results   []any `json:"results"`
		Providers *int  `json:"providers"`
	}
	if code := doJSON(t, http.MethodGet, srv.URL+"/api/v1/series/lookup?q=frieren", "", &res); code != 200 {
		t.Fatalf("lookup: %d", code)
	}
	if res.Providers == nil || *res.Providers != 0 || len(res.Results) != 0 {
		t.Fatalf("want 0 providers and no results, got %+v", res)
	}
	// a switched-off module isn't searched either
	body := `{"kind":"metadata","implementation":"anilist","name":"AniList","enabled":false,"priority":1,"settings":{}}`
	if code := doJSON(t, http.MethodPost, srv.URL+"/api/v1/modules", body, nil); code != 200 {
		t.Fatalf("create module: %d", code)
	}
	res.Providers = nil
	doJSON(t, http.MethodGet, srv.URL+"/api/v1/series/lookup?q=frieren", "", &res)
	if res.Providers == nil || *res.Providers != 0 {
		t.Fatalf("a disabled module counted: %+v", res)
	}
}
