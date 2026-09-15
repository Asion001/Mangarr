package reading

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"sync"

	"github.com/Asion001/mangarr/internal/imagecheck"
)

// Bounds is a page's size and the box inside its uniform borders (white or
// black margins and scan edges), for the web reader's "crop borders".
type Bounds struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	X      int `json:"x"`
	Y      int `json:"y"`
	W      int `json:"w"`
	H      int `json:"h"`
}

// boundsCache remembers computed bounds (keyed by file or release and page).
type boundsCache struct {
	mu sync.Mutex
	m  map[string]Bounds
}

func (c *boundsCache) get(k string) (Bounds, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.m[k]
	return b, ok
}

func (c *boundsCache) put(k string, b Bounds) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil || len(c.m) > 20000 {
		c.m = map[string]Bounds{}
	}
	c.m[k] = b
}

// PageBounds returns page n's size and content box.
func (s *Service) PageBounds(ctx context.Context, b *BookInfo, n int) (Bounds, error) {
	key := fmt.Sprintf("ch%d|%d", b.Chapter.ID, n)
	if b.File != nil {
		key = fmt.Sprintf("f%d|%d|%d", b.File.ID, b.File.ImportedAt.Unix(), n)
	}
	if bd, ok := s.bounds.get(key); ok {
		return bd, nil
	}
	data, _, err := s.Page(ctx, b, n)
	if err != nil {
		return Bounds{}, err
	}
	var img image.Image
	if info, _ := imagecheck.Detect(data); info.Format == "jxl" {
		img, err = decodeJXL(data)
	} else {
		img, _, err = image.Decode(bytes.NewReader(data))
	}
	if err != nil {
		return Bounds{}, fmt.Errorf("decode page %d: %w", n, err)
	}
	bd := ContentBox(img)
	s.bounds.put(key, bd)
	return bd, nil
}

// ContentBox finds the box inside an image's uniform borders. A side is
// trimmed while its rows (or columns) are nearly all white or nearly all
// black; the box keeps a little margin, and pages that would lose most of
// themselves (blank or very light pages) aren't cropped.
func ContentBox(img image.Image) Bounds {
	r := img.Bounds()
	w, h := r.Dx(), r.Dy()
	full := Bounds{Width: w, Height: h, W: w, H: h}
	if w < 16 || h < 16 {
		return full
	}
	luma := func(x, y int) int {
		cr, cg, cb, _ := img.At(r.Min.X+x, r.Min.Y+y).RGBA()
		return int((299*cr + 587*cg + 114*cb) / 1000 >> 8)
	}
	stepX, stepY := max(1, w/300), max(1, h/300)
	// uniform: nearly every sample is light (>= 225) or nearly every one is dark (<= 30)
	uniform := func(samples func(yield func(int))) bool {
		n, light, dark := 0, 0, 0
		samples(func(l int) {
			n++
			if l >= 225 {
				light++
			} else if l <= 30 {
				dark++
			}
		})
		limit := n - max(1, n/100) // allow 1% specks
		return light >= limit || dark >= limit
	}
	row := func(y int) bool {
		return uniform(func(yield func(int)) {
			for x := 0; x < w; x += stepX {
				yield(luma(x, y))
			}
		})
	}
	col := func(x, y0, y1 int) bool {
		return uniform(func(yield func(int)) {
			for y := y0; y < y1; y += stepY {
				yield(luma(x, y))
			}
		})
	}
	top, bottom := 0, h
	for top < h && row(top) {
		top++
	}
	for bottom > top && row(bottom-1) {
		bottom--
	}
	left, right := 0, w
	for left < w && col(left, top, bottom) {
		left++
	}
	for right > left && col(right-1, top, bottom) {
		right--
	}
	cw, ch := right-left, bottom-top
	if cw < w*3/10 || ch < h*3/10 { // blank or nearly blank: leave it
		return full
	}
	// keep a small margin around the content
	mx, my := w/100, h/100
	left, top = max(0, left-mx), max(0, top-my)
	right, bottom = min(w, right+mx), min(h, bottom+my)
	return Bounds{Width: w, Height: h, X: left, Y: top, W: right - left, H: bottom - top}
}
