package komgaapi

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/envcfg"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/reading"
	"github.com/Asion001/mangarr/internal/settings"
)

func TestWebRedirects(t *testing.T) {
	dbtest.ForEachDialect(t, func(t *testing.T, d *db.DB) {
		for _, tc := range []struct {
			name, publicURL, envURL, host, listen, urlBase, wantBase string
			tls                                                      bool
		}{
			{name: "defaults", host: "shelf.example:25600", wantBase: "http://shelf.example:8787"},
			{name: "custom port", host: "shelf.example:25600", listen: "0.0.0.0:9090", wantBase: "http://shelf.example:9090"},
			{name: "host without port", host: "shelf.example", wantBase: "http://shelf.example:8787"},
			{name: "IPv4", host: "192.0.2.1:25600", wantBase: "http://192.0.2.1:8787"},
			{name: "IPv6", host: "[2001:db8::1]:25600", wantBase: "http://[2001:db8::1]:8787"},
			{name: "IPv6 without port", host: "[2001:db8::1]", wantBase: "http://[2001:db8::1]:8787"},
			{name: "TLS", host: "shelf.example:25600", tls: true, wantBase: "https://shelf.example:8787"},
			{name: "base path", host: "shelf.example:25600", urlBase: "/shelf/", wantBase: "http://shelf.example:8787/shelf"},
			{name: "public URL", publicURL: "https://reader.example:9443/", host: "shelf.example:25600", wantBase: "https://reader.example:9443"},
			{name: "public base path", publicURL: "https://reader.example/shelf/", host: "shelf.example:25600", urlBase: "/shelf", wantBase: "https://reader.example/shelf"},
			{name: "environment overrides settings", publicURL: "https://old.example", envURL: "https://reader.example/new/", host: "shelf.example:25600", wantBase: "https://reader.example/new"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				st := settings.NewStore(d)
				g := settings.DefaultGeneral()
				g.PublicURL = tc.publicURL
				if err := st.Set(context.Background(), settings.KeyGeneral, g); err != nil {
					t.Fatal(err)
				}
				if tc.envURL != "" {
					if err := envcfg.ApplySettings(map[string]string{"MANGARR_GENERAL_PUBLIC_URL": tc.envURL}, st); err != nil {
						t.Fatal(err)
					}
				}
				s := testService()
				s.deps.Settings, s.deps.WebListen, s.deps.URLBase = st, tc.listen, tc.urlBase
				// No series or chapters exist: even unknown IDs must redirect.
				for _, route := range []struct{ path, target string }{
					{"/series/9123", "/series/9123"},
					{"/series/9123/", "/series/9123"},
					{"/book/9456", "/read/9456"},
					{"/books/9456", "/read/9456"},
					{"/book/9456?api_key=unused&page=3", "/read/9456"},
					{"/readlists/continue-reading", "/"},
					{"/series/unknown", "/series/unknown"},
				} {
					req := httptest.NewRequest(http.MethodGet, "http://"+tc.host+route.path, nil)
					if tc.tls {
						req.TLS = &tls.ConnectionState{}
					}
					rec := httptest.NewRecorder()
					s.Handler().ServeHTTP(rec, req)
					if rec.Code != http.StatusFound || rec.Header().Get("Location") != tc.wantBase+route.target {
						t.Errorf("%s: got %d %q, want 302 %q", route.path, rec.Code, rec.Header().Get("Location"), tc.wantBase+route.target)
					}
					if rec.Header().Get("WWW-Authenticate") != "" || len(rec.Result().Cookies()) != 0 {
						t.Errorf("%s: redirect must not challenge or establish an API session", route.path)
					}
				}
			})
		}
	})
}

func TestWebRedirectIDsMatchKomgaDTOs(t *testing.T) {
	ser := &model.Series{ID: 123, Title: "Cloud Orchard", RootFolderID: 7}
	series := toSeries(reading.SeriesInfo{Series: *ser})
	book := toBook(reading.BookInfo{Chapter: model.Chapter{ID: 456, SeriesID: ser.ID}}, ser, "", 0)
	if series.ID != "123" || book.ID != "456" || book.SeriesID != series.ID {
		t.Fatalf("Komga IDs must match mangarr series/chapter IDs: series=%s, book=%s, book series=%s", series.ID, book.ID, book.SeriesID)
	}
}
