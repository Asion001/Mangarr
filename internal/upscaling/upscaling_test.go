package upscaling

import (
	"testing"

	"github.com/Asion001/mangarr/internal/downloads"
)

func TestChooseScale(t *testing.T) {
	cases := []struct {
		w, min int
		scales []int
		want   int
	}{
		{720, 1400, []int{2, 4, 8}, 2},
		{600, 1400, []int{2, 4, 8}, 4},
		{600, 1400, []int{2, 3, 4}, 3},
		{200, 1400, []int{2, 3}, 3},
		{700, 1400, []int{4}, 4},
		{700, 1400, []int{1}, 0},
	}
	for _, c := range cases {
		if got := ChooseScale(c.w, c.min, c.scales); got != c.want {
			t.Errorf("ChooseScale(%d,%d,%v)=%d want %d", c.w, c.min, c.scales, got, c.want)
		}
	}
}

func TestNeedsUpscale(t *testing.T) {
	if !NeedsUpscale(downloads.PageFile{Width: 800, Format: "jpeg"}, 1400) {
		t.Error("narrow jpeg should be upscaled")
	}
	if NeedsUpscale(downloads.PageFile{Width: 1600, Format: "jpeg"}, 1400) {
		t.Error("wide page must be left alone")
	}
	if NeedsUpscale(downloads.PageFile{Width: 500, Format: "gif"}, 1400) {
		t.Error("gif must be left alone")
	}
	if NeedsUpscale(downloads.PageFile{Width: 0, Format: "avif"}, 1400) {
		t.Error("unknown width must be left alone")
	}
}

func TestOutputFormat(t *testing.T) {
	for _, c := range []struct{ profile, page, want string }{
		{"", "jpeg", "webp"},
		{"jpeg", "png", "jpeg"},
		{SourceFormat, "jpeg", "jpeg"},
		{SourceFormat, "webp", "webp"},
		{SourceFormat, "png", "png"},
		{SourceFormat, "bmp", "png"},
	} {
		if got := OutputFormat(c.profile, c.page); got != c.want {
			t.Errorf("OutputFormat(%q, %q) = %q, want %q", c.profile, c.page, got, c.want)
		}
	}
}
