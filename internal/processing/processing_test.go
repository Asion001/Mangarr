package processing

import (
	"testing"

	"github.com/Asion001/mangarr/internal/downloads"
)

func TestChangedPageCount(t *testing.T) {
	original := []downloads.PageFile{
		{Name: "0001.jpg", Path: "/work/0001.jpg", Format: "jpeg", Width: 800, Height: 1200},
		{Name: "0002.jpg", Path: "/work/0002.jpg", Format: "jpeg", Width: 800, Height: 1200},
	}
	if got := changedPageCount(original, append([]downloads.PageFile(nil), original...)); got != 0 {
		t.Fatalf("unchanged pages = %d, want 0", got)
	}
	processed := append([]downloads.PageFile(nil), original...)
	processed[1] = downloads.PageFile{Name: "0002.avif", Path: "/work/encoded/0002.avif", Format: "avif", Width: 800, Height: 1200}
	if got := changedPageCount(original, processed); got != 1 {
		t.Fatalf("changed pages = %d, want 1", got)
	}
}
