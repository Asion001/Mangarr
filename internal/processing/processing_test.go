package processing

import (
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/model"
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

func writePNG(t *testing.T, dir, name string, w, h int) downloads.PageFile {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return downloads.PageFile{Name: name, Path: path, Format: "png", Width: w, Height: h}
}

func TestShrinkWidePagesLeaveJunkAlone(t *testing.T) {
	dir := t.TempDir()
	pages := []downloads.PageFile{
		writePNG(t, dir, "0001.png", 1000, 1500), // too wide: shrunk to 500
		writePNG(t, dir, "0002.png", 1600, 1000), // a spread may be twice as wide: 1000
		writePNG(t, dir, "0003.png", 100, 100),   // junk
		writePNG(t, dir, "0004.png", 400, 600),   // fine as it is
	}
	cfg := model.ProfileConfig{Pages: model.PageRules{MaxWidth: 500}}
	res, err := New(nil, nil).Process(context.Background(), cfg, pages, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := [][2]int{{500, 750}, {1000, 625}, {100, 100}, {400, 600}}
	for i, pg := range res.Pages {
		if pg.Width != want[i][0] || pg.Height != want[i][1] {
			t.Errorf("page %d: %d×%d, want %d×%d", i+1, pg.Width, pg.Height, want[i][0], want[i][1])
		}
	}
	if res.Shrunk != 2 || res.ProcessedPages != 2 || !res.Changed {
		t.Fatalf("result: %+v", res)
	}
	if res.Pages[2].Path != pages[2].Path {
		t.Fatal("junk page was rewritten")
	}
}

func TestProcessParamsKeepsOlderHashes(t *testing.T) {
	old := model.ProfileConfig{Encode: model.EncodeConfig{Format: "avif", Preset: "balanced"}}
	withDefaults := old
	withDefaults.Pages = model.PageRules{JunkUnder: model.DefaultJunkUnder}
	if old.ProcessParams() != withDefaults.ProcessParams() {
		t.Fatal("default page rules must not change the hash of existing profiles")
	}
	shrinking := old
	shrinking.Pages.MaxWidth = 2048
	if old.ProcessParams() == shrinking.ProcessParams() {
		t.Fatal("a width limit is a processing change")
	}
	if (model.ProfileConfig{Pages: model.PageRules{MaxWidth: 2048}}).ProcessParams() == "" {
		t.Fatal("shrinking alone needs processing")
	}
	splitting := old
	splitting.Pages.SplitTall = true
	if old.ProcessParams() == splitting.ProcessParams() || splitting.ProcessParams() == "" {
		t.Fatal("tall-page splitting must be a processing change")
	}
	defaultHeight, explicitHeight := splitting, splitting
	explicitHeight.Pages.MaxHeight = model.DefaultSplitHeight
	if defaultHeight.ProcessParams() != explicitHeight.ProcessParams() {
		t.Fatal("the default split height must have one stable processing hash")
	}
}
