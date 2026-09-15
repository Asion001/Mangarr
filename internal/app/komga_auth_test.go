package app_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/komgaapi"
	"github.com/Asion001/mangarr/internal/settings"
)

// TestKomgaAPILogins walks the login flows of the Mihon Komga extension
// (401 challenge → Basic → cookie, which Mihon's tracker then relies on)
// and KMReader (users/me → X-Auth-Token → an API key of its own).
func TestKomgaAPILogins(t *testing.T) {
	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	if _, err := e.App.Auth.CreateUser(e.Ctx, "ann", "secret-pass"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(e.App.Komga.Handler())
	defer srv.Close()
	do := func(c *http.Client, method, path string, hdr map[string]string, body string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// Mihon: OkHttp retries with Basic only after a 401 challenge
	jar, _ := cookiejar.New(nil)
	mihon := &http.Client{Jar: jar}
	resp := do(mihon, "GET", "/api/v1/libraries", nil, "")
	if resp.StatusCode != 401 || resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("challenge: %d %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/libraries", nil)
	req.SetBasicAuth("ann", "secret-pass")
	req.Header.Set("User-Agent", "TachiyomiKomga/1.4.69")
	resp, err := mihon.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("basic: %v %d", err, resp.StatusCode)
	}
	var libs []map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&libs)
	if len(libs) != 1 || libs[0]["id"] == nil || libs[0]["name"] == nil || libs[0]["root"] == nil {
		t.Fatalf("libraries: %v", libs)
	}
	// the tracker sends no credentials, only the cookie
	if resp := do(mihon, "GET", "/api/v2/users/me", map[string]string{"User-Agent": "Mihon v0.19"}, ""); resp.StatusCode != 200 {
		t.Fatalf("cookie session: %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("GET", srv.URL+"/api/v1/libraries", nil)
	req.SetBasicAuth("ann", "wrong")
	if resp, _ := http.DefaultClient.Do(req); resp.StatusCode != 401 {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}

	// KMReader: login, keep the X-Auth-Token, then create its own API key
	req, _ = http.NewRequest("GET", srv.URL+"/api/v2/users/me?remember-me=true", nil)
	req.SetBasicAuth("ann", "secret-pass")
	resp, _ = http.DefaultClient.Do(req)
	token := resp.Header.Get("X-Auth-Token")
	var me map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&me)
	if resp.StatusCode != 200 || token == "" || me["email"] != "ann" || me["id"] == nil || me["roles"] == nil {
		t.Fatalf("users/me: %d %q %v", resp.StatusCode, token, me)
	}
	resp = do(http.DefaultClient, "POST", "/api/v2/users/me/api-keys", map[string]string{"X-Auth-Token": token, "User-Agent": "KMReader/2.1"},
		`{"comment":"KMReader iPad"}`)
	var key map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&key)
	for _, f := range []string{"id", "userId", "key", "comment", "createdDate", "lastModifiedDate"} {
		if key[f] == nil {
			t.Fatalf("api key lacks %s: %v", f, key)
		}
	}
	apiKey := key["key"].(string)
	if resp := do(http.DefaultClient, "GET", "/api/v1/libraries", map[string]string{"X-API-Key": apiKey}, ""); resp.StatusCode != 200 {
		t.Fatalf("api key: %d", resp.StatusCode)
	}
	if resp := do(http.DefaultClient, "GET", "/api/v1/libraries", map[string]string{"X-API-Key": apiKey + "x"}, ""); resp.StatusCode != 401 {
		t.Fatalf("bad key: %d", resp.StatusCode)
	}
	// Paperback only does Basic: any username, the key as the password
	for pass, want := range map[string]int{apiKey: 200, apiKey + "x": 401} {
		req, _ := http.NewRequest("GET", srv.URL+"/api/v2/users/me", nil)
		req.SetBasicAuth("whatever", pass)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != want {
			t.Fatalf("basic with key: %v %d, want %d", err, resp.StatusCode, want)
		}
	}
	// a session issued for a key dies with the key
	resp = do(http.DefaultClient, "GET", "/api/v1/libraries", map[string]string{"X-API-Key": apiKey}, "")
	keyToken := resp.Header.Get("X-Auth-Token")
	if keyToken == "" {
		t.Fatal("no session for the key")
	}
	if resp := do(http.DefaultClient, "DELETE", "/api/v2/users/me/api-keys/"+key["id"].(string), map[string]string{"X-Auth-Token": token}, ""); resp.StatusCode != 204 {
		t.Fatalf("delete key: %d", resp.StatusCode)
	}
	if resp := do(http.DefaultClient, "GET", "/api/v1/libraries", map[string]string{"X-Auth-Token": keyToken}, ""); resp.StatusCode != 401 {
		t.Fatalf("token of a deleted key: %d", resp.StatusCode)
	}
	resp = do(http.DefaultClient, "GET", "/api/v1/nope", map[string]string{"X-Auth-Token": token}, "")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 404 || !strings.Contains(string(body), "violations") {
		t.Fatalf("not found: %d %s", resp.StatusCode, body)
	}
}

// TestKomgaAPIListener: the listener follows the setting without a restart.
func TestKomgaAPIListener(t *testing.T) {
	e := newTestApp(t, dbtest.DSNs(t)["sqlite"])
	svc := komgaapi.NewService(komgaapi.Deps{DB: e.App.DB, Settings: e.App.Settings, Auth: e.App.Auth, Log: e.App.Log}, "127.0.0.1:0")
	if err := svc.Start(e.Ctx); err != nil {
		t.Fatal(err)
	}
	if st := svc.Status(e.Ctx); st.Listening || st.Enabled {
		t.Fatalf("off by default: %+v", st)
	}
	rd := settings.DefaultReading()
	rd.Enabled = true
	_ = e.App.Settings.Set(e.Ctx, settings.KeyReading, rd)
	svc.Reconcile(e.Ctx)
	st := svc.Status(e.Ctx)
	if !st.Listening {
		t.Fatalf("not listening: %+v", st)
	}
	resp, err := http.Get("http://" + st.Address + "/api/v1/client-settings/global/list")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("serving: %v", err)
	}
	rd.Enabled = false
	_ = e.App.Settings.Set(e.Ctx, settings.KeyReading, rd)
	svc.Reconcile(e.Ctx)
	if st := svc.Status(e.Ctx); st.Listening {
		t.Fatalf("still listening: %+v", st)
	}
	c := &http.Client{Timeout: time.Second}
	if _, err := c.Get("http://" + st.Address + "/api/v1/client-settings/global/list"); err == nil {
		t.Fatal("stopped listener still answers")
	}
}
