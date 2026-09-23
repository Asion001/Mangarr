package api_test

import (
	"context"
	"testing"

	"github.com/Asion001/mangarr/internal/auth"
	"github.com/Asion001/mangarr/internal/reading"
)

func TestMyReadingStats(t *testing.T) {
	srv, app := newServer(t, false)
	if _, err := app.Auth.CreateUser(context.Background(), auth.NewUser{Username: "reader", Password: "reader-pass-1"}); err != nil {
		t.Fatal(err)
	}
	me := caller{t, login(t, srv.URL, "reader", "reader-pass-1"), srv.URL}
	var stats reading.ReadingStats
	if code := me.do("GET", "/api/v1/me/reading-stats", "", &stats); code != 200 {
		t.Fatalf("reading stats: %d", code)
	}
	if stats.TotalActiveSeconds != 0 || stats.CompletedChapters != 0 || stats.ActiveMonth != nil || stats.TopSeries != nil ||
		stats.Languages == nil || stats.Genres == nil {
		t.Fatalf("empty stats %+v", stats)
	}
}
