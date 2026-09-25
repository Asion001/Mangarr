package mediaserver

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/model"
)

func TestMatch(t *testing.T) {
	a := model.Adaptation{Title: "Ｃloud: Lantern!", Format: "tv", Year: 2024, ExternalIDs: map[string]string{"anilist": "700001", "mal": "800001"}}
	title := Item{ID: "title", Title: "cloud lantern", Type: "series", Year: 2024, URL: "title"}
	exact := Item{ID: "id", Title: "Different localization", Type: "series", URL: "id", ProviderIDs: map[string]string{"AniList": "700001"}}
	for _, tt := range []struct {
		name  string
		items []Item
		want  string
	}{
		{"title normalization", []Item{title}, "title"},
		{"id outranks title regardless of year", []Item{title, exact}, "id"},
		{"MAL alias", []Item{{ID: "id", Type: "series", URL: "mal", ProviderIDs: map[string]string{"MyAnimeList": "800001"}}}, "mal"},
		{"wrong format", []Item{{ID: "id", Type: "movie", URL: "movie", ProviderIDs: exact.ProviderIDs}}, ""},
		{"ambiguous title", []Item{title, {ID: "other", Title: title.Title, Type: title.Type, Year: title.Year, URL: "other"}}, ""},
		{"duplicate item", []Item{title, title}, "title"},
		{"conflicting ids", []Item{{ID: "id", Title: title.Title, Type: title.Type, Year: title.Year, URL: "bad", ProviderIDs: map[string]string{"AniList": "700009"}}}, ""},
		{"inconsistent ids", []Item{{ID: "id", Type: "series", URL: "bad", ProviderIDs: map[string]string{"AniList": "700001", "MAL": "800009"}}}, ""},
		{"wrong year", []Item{{ID: "id", Title: title.Title, Type: title.Type, Year: 2023, URL: "bad"}}, ""},
		{"original title", []Item{{ID: "id", Title: "Different name", OriginalTitle: title.Title, Type: title.Type, Year: title.Year, URL: "original"}}, "original"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Match(a, tt.items); got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
	a.Year = 0
	if got := Match(a, []Item{title}); got != "" {
		t.Fatal("unknown year must not match by title")
	}
	for _, format := range []string{"ova", "ona", "special"} {
		a.Format = format
		if Match(a, []Item{exact}) != "id" {
			t.Fatal(format)
		}
	}
}

func TestCache(t *testing.T) {
	var c Cache[string]
	var calls atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{})
	load := func(context.Context) (string, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return "found", nil
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if v, err := c.Get(context.Background(), "key", load); err != nil || v != "found" {
				t.Errorf("%q %v", v, err)
			}
		})
	}
	<-started
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Get(cancelled, "key", load); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
	c.entries["key"] = entry[string]{until: time.Now().Add(-time.Second)}
	if _, err := c.Get(context.Background(), "key", load); err != nil || calls.Load() != 2 {
		t.Fatal(err, calls.Load())
	}
	for _, fail := range []bool{false, true} {
		var cached Cache[string]
		n := 0
		for range 2 {
			_, _ = cached.Get(context.Background(), "miss", func(context.Context) (string, error) {
				n++
				if fail {
					return "", errors.New("offline")
				}
				return "", nil
			})
		}
		if n != 1 {
			t.Fatal("miss/failure not cached", n)
		}
	}
	c.entries = map[string]entry[string]{}
	for i := range maxEntries + 1 {
		_, _ = c.Get(context.Background(), string(rune(i)), func(context.Context) (string, error) { return "", nil })
	}
	if len(c.entries) > maxEntries {
		t.Fatal(len(c.entries))
	}
}

func TestSettings(t *testing.T) {
	for _, address := range []string{"file:///tmp/media", "https://key@media.example", "https://media.example?key=secret", "https://media.example#key", "https://media.example?", "http://"} {
		if (&Settings{URL: address, APIKey: "test-key"}).Validate() == nil {
			t.Fatal(address)
		}
	}
	if err := (&Settings{URL: "http://127.0.0.1:8096/base", APIKey: "test-key"}).Validate(); err != nil {
		t.Fatal(err)
	}
}
