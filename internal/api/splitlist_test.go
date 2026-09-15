package api

import (
	"slices"
	"testing"
)

func TestSplitList(t *testing.T) {
	got := splitList([]string{"failed", "queued,paused", " ", "a, b"})
	if !slices.Equal(got, []string{"failed", "queued", "paused", "a", "b"}) {
		t.Fatalf("got %q", got)
	}
	if splitList(nil) != nil {
		t.Fatal("empty input")
	}
}
