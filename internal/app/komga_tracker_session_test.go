package app_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/komgaapi"
	"github.com/Asion001/mangarr/internal/model"
)

// The extension sends X-API-Key; KomgaApi in Mihon sends only its user agent
// and the shared cookies. Exercise that transition, including stale cookies.
func TestMihonTrackerSessionRecovery(t *testing.T) {
	for dialect, dsn := range dbtest.DSNs(t) {
		t.Run(dialect, func(t *testing.T) {
			f := newKomgaFixture(t, dsn)
			base, _ := url.Parse(f.srv.URL)
			otherKey, _, err := f.e.App.Komga.CreateKey(f.e.Ctx, 0, "other device", "test")
			if err != nil {
				t.Fatal(err)
			}
			otherReq, _ := http.NewRequest("GET", f.srv.URL+"/api/v1/libraries", nil)
			otherReq.Header.Set("X-API-Key", otherKey)
			otherResp, err := http.DefaultClient.Do(otherReq)
			if err != nil {
				t.Fatal(err)
			}
			otherToken := otherResp.Header.Get("X-Auth-Token")
			otherResp.Body.Close()
			otherReq.Header.Set("X-API-Key", f.key)
			ownResp, err := http.DefaultClient.Do(otherReq)
			if err != nil {
				t.Fatal(err)
			}
			ownToken := ownResp.Header.Get("X-Auth-Token")
			ownResp.Body.Close()
			// Sign real expired and near-expiry tokens, rather than merely
			// passing malformed strings through the invalid-token branch.
			datedToken := func(exp time.Time) string {
				payloadText, _, _ := strings.Cut(ownToken, ".")
				payload, err := base64.RawURLEncoding.DecodeString(payloadText)
				if err != nil || len(payload) != 24 {
					t.Fatal("invalid fixture token")
				}
				binary.BigEndian.PutUint64(payload[16:], uint64(exp.Unix()))
				general, err := f.e.App.Settings.General(f.e.Ctx)
				if err != nil {
					t.Fatal(err)
				}
				key := sha256.Sum256([]byte("komga-session:" + general.SessionSecret))
				mac := hmac.New(sha256.New, key[:])
				mac.Write(payload)
				return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
			}
			for _, tc := range []struct{ name, cookie, header string }{{"missing", "", ""}, {"invalid cookie", "stale-cookie", ""}, {"expired", datedToken(time.Now().Add(-time.Hour)), ""}, {"near expiry", datedToken(time.Now().Add(time.Hour)), ""}, {"different device", otherToken, ""}, {"invalid header", "stale-cookie", "invalid-token"}, {"header only", "", otherToken}} {
				t.Run(tc.name, func(t *testing.T) {
					jar, _ := cookiejar.New(nil)
					if tc.cookie != "" {
						jar.SetCookies(base, []*http.Cookie{{Name: komgaapi.SessionCookie, Value: tc.cookie, Path: "/"}})
					}
					client := &http.Client{Jar: jar}
					request := func(method, path, body string, extension bool) *http.Response {
						t.Helper()
						req, _ := http.NewRequest(method, f.srv.URL+path, strings.NewReader(body))
						req.Header.Set("Content-Type", "application/json")
						req.Header.Set("User-Agent", "Mihon v0.19")
						if extension {
							req.Header.Set("X-API-Key", f.key)
							req.Header.Set("X-Auth-Token", tc.header)
						}
						resp, err := client.Do(req)
						if err != nil {
							t.Fatal(err)
						}
						return resp
					}
					// Catalog path the extension actually uses, then the two tracker reads.
					resp := request("GET", "/api/v1/series/"+sid(f.ser.ID)+"/books?unpaged=true&media_status=READY&deleted=false", "", true)
					resp.Body.Close()
					if resp.StatusCode != 200 || resp.Header.Get("Set-Cookie") == "" {
						t.Fatalf("extension did not repair session: %d", resp.StatusCode)
					}
					for _, path := range []string{"/api/v1/series/" + sid(f.ser.ID), "/api/v2/series/" + sid(f.ser.ID) + "/read-progress/tachiyomi"} {
						resp = request("GET", path, "", false)
						resp.Body.Close()
						if resp.StatusCode != 200 {
							t.Fatalf("cookie-only tracker %s: %d", path, resp.StatusCode)
						}
					}
					resp = request("PUT", "/api/v2/series/"+sid(f.ser.ID)+"/read-progress/tachiyomi", `{"lastBookNumberSortRead":3}`, false)
					resp.Body.Close()
					if resp.StatusCode != 204 {
						t.Fatalf("tracker update: %d", resp.StatusCode)
					}
					resp = request("GET", "/api/v2/series/"+sid(f.ser.ID)+"/read-progress/tachiyomi", "", false)
					var progress struct {
						Read int `json:"booksReadCount"`
					}
					err = json.NewDecoder(resp.Body).Decode(&progress)
					resp.Body.Close()
					if err != nil || progress.Read != 3 {
						t.Fatalf("tracker progress: %+v %v", progress, err)
					}
				})
			}
			// Preserve chapter zero, fractional numbering and gaps; v1 is positional.
			for i, n := range []float64{0, 0.5, 2, 7} {
				_, err := f.e.App.DB.NewUpdate().Model((*model.Chapter)(nil)).Set("number_sort = ?", n).Where("id = ?", f.chs[i].ID).Exec(f.e.Ctx)
				if err != nil {
					t.Fatal(err)
				}
			}
			var v2 map[string]any
			f.get(t, "/api/v2/series/"+sid(f.ser.ID)+"/read-progress/tachiyomi", &v2)
			if v2["maxNumberSort"] != 7.0 || v2["lastReadContinuousNumberSort"] != 2.0 {
				t.Fatalf("numbered tracker: %v", v2)
			}
			var v1 map[string]any
			f.get(t, "/api/v1/series/"+sid(f.ser.ID)+"/read-progress/tachiyomi", &v1)
			if v1["lastReadContinuousIndex"] != 3.0 {
				t.Fatalf("indexed tracker: %v", v1)
			}
		})
	}
}
