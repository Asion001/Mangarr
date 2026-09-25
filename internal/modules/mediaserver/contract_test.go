package mediaserver_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
	"github.com/Asion001/mangarr/internal/modules/mediaserver"
	_ "github.com/Asion001/mangarr/internal/modules/mediaserver/jellyfin"
	_ "github.com/Asion001/mangarr/internal/modules/mediaserver/silo"
)

func instance(t *testing.T, kind, address string) mediaserver.Module {
	t.Helper()
	impl, ok := modules.Lookup(modules.KindMediaServer, kind)
	if !ok {
		t.Fatal(kind)
	}
	s, err := modules.DecodeSettings(impl, map[string]any{"url": address, "apiKey": "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := impl.New(modules.Deps{HTTP: http.DefaultClient}, s)
	if err != nil {
		t.Fatal(err)
	}
	return m.(mediaserver.Module)
}

func TestJellyfin(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("X-Emby-Token") != "test-secret" || strings.Contains(r.URL.RawQuery, "test-secret") {
			t.Error("missing or leaked credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/base/System/Info":
			io.WriteString(w, `{"Id":"test-server"}`)
		case "/base/Items":
			q := r.URL.Query()
			if q.Get("Recursive") != "true" || q.Get("IncludeItemTypes") != "Series,Movie" || q.Get("Fields") != "ProviderIds,OriginalTitle" || q.Get("ExcludeLocationTypes") != "Virtual" {
				t.Error(q)
			}
			switch q.Get("StartIndex") {
			case "0":
				io.WriteString(w, `{"TotalRecordCount":5,"Items":[{"Id":"wrong","Name":"Cloud Lantern","Type":"Series","ProductionYear":2024},{"Id":"virtual","Type":"Series","LocationType":"Virtual","ProviderIds":{"AniList":"700001"}}]}`)
			case "2":
				io.WriteString(w, `{"TotalRecordCount":5,"Items":[{"Id":"matched","Name":"Other localization","Type":"Series","ProviderIds":{"AniList":"700001"}},{"Id":"movie","Name":"Cloud-Lantern: Voyage","Type":"Movie","ProductionYear":2025},{"Id":"ova","Name":"Other short","Type":"Movie","ProviderIds":{"MyAnimeList":"800002"}}]}`)
			default:
				t.Error(q)
				http.Error(w, "bad offset", 400)
			}
		default:
			t.Error(r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	m := instance(t, "jellyfin", server.URL+"/base/")
	if err := m.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		a  model.Adaptation
		id string
	}{
		{model.Adaptation{Title: "Cloud Lantern", Format: "tv", Year: 2024, ExternalIDs: map[string]string{"anilist": "700001"}}, "matched"},
		{model.Adaptation{Title: "Cloud Lantern Voyage", Format: "movie", Year: 2025}, "movie"},
		{model.Adaptation{Format: "ova", ExternalIDs: map[string]string{"mal": "800002"}}, "ova"},
		{model.Adaptation{Title: "Cloud Lantern Voyage", Format: "movie", Year: 2026}, ""},
	} {
		for range 2 {
			got, err := m.Find(context.Background(), tt.a)
			if err != nil {
				t.Fatal(err)
			}
			want := ""
			if tt.id != "" {
				want = server.URL + "/base/web/index.html#!/details?id=" + tt.id
			}
			if got != want {
				t.Fatalf("%q != %q", got, want)
			}
		}
	}
	if requests.Load() != 3 {
		t.Fatal("catalog not cached", requests.Load())
	}
}

func TestSilo(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-secret" || strings.Contains(r.URL.RawQuery, "test-secret") {
			t.Error("credentials")
		}
		if r.URL.Path != "/base/api/v1/catalog" {
			t.Error(r.URL.Path)
		}
		q := r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		if q.Get("limit") == "1" {
			io.WriteString(w, `{"items":[],"has_more":false}`)
			return
		}
		if q.Get("q") == "Absent Title" {
			io.WriteString(w, `{"items":[],"has_more":false}`)
			return
		}
		if q.Get("type") != "movie" || q.Get("year_min") != "2025" || q.Get("year_max") != "2025" || q.Get("q") != "Cloud Lantern Voyage" {
			t.Error(q)
		}
		switch q.Get("offset") {
		case "0":
			io.WriteString(w, `{"items":[{"content_id":"wrong","title":"Cloud Lantern Voyage","year":2024,"type":"movie"}],"has_more":true,"snapshot":"2026-01-01T00:00:00Z"}`)
		case "1":
			if q.Get("snapshot") != "2026-01-01T00:00:00Z" {
				t.Error(q)
			}
			io.WriteString(w, `{"items":[{"content_id":"movie:cloud/lantern?x#y","title":"Cloud-Lantern: Voyage","year":2025,"type":"movie"}],"has_more":false}`)
		default:
			t.Error(q)
			http.Error(w, "bad offset", 400)
		}
	}))
	defer server.Close()
	m := instance(t, "silo", server.URL+"/base")
	if err := m.Test(context.Background()); err != nil {
		t.Fatal(err)
	}
	a := model.Adaptation{Title: "Cloud Lantern Voyage", Format: "movie", Year: 2025, ExternalIDs: map[string]string{"anilist": "700001"}}
	for range 2 {
		got, err := m.Find(context.Background(), a)
		want := server.URL + "/base/item/" + url.PathEscape("movie:cloud/lantern?x#y")
		if err != nil || got != want {
			t.Fatalf("%s %v (want %s)", got, err, want)
		}
	}
	a.Title = "Absent Title"
	for range 2 {
		got, err := m.Find(context.Background(), a)
		if err != nil || got != "" {
			t.Fatal(got, err)
		}
	}
	a.Year = 0
	if got, err := m.Find(context.Background(), a); err != nil || got != "" {
		t.Fatal(got, err)
	}
	if requests.Load() != 4 {
		t.Fatal("results not cached", requests.Load())
	}
}

func TestFailuresAndRedirects(t *testing.T) {
	for _, kind := range []string{"jellyfin", "silo"} {
		for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError, http.StatusOK, http.StatusFound} {
			t.Run(fmt.Sprintf("%s/%d", kind, status), func(t *testing.T) {
				var hits, leaks atomic.Int32
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaks.Add(1) }))
				defer target.Close()
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits.Add(1)
					w.Header().Set("Location", target.URL)
					w.WriteHeader(status)
					io.WriteString(w, "test-secret invalid response")
				}))
				defer server.Close()
				m := instance(t, kind, server.URL)
				if err := m.Test(context.Background()); err == nil || strings.Contains(err.Error(), "test-secret") {
					t.Fatal(err)
				}
				a := model.Adaptation{Title: "Cloud Lantern", Format: "tv", Year: 2024}
				for range 2 {
					got, err := m.Find(context.Background(), a)
					if err == nil || got != "" || strings.Contains(err.Error(), "test-secret") {
						t.Fatal(got, err)
					}
				}
				if hits.Load() != 2 || leaks.Load() != 0 {
					t.Fatal(hits.Load(), leaks.Load())
				}
			})
		}
	}
}
