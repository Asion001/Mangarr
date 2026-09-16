package reading

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"runtime"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/apitiming"
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

// boundsKey identifies a page's bounds: its file (and when it was imported)
// or, for a chapter that isn't downloaded, the chapter itself.
func boundsKey(b *BookInfo, n int) string {
	if b.File != nil {
		return fmt.Sprintf("f%d|%d|%d", b.File.ID, b.File.ImportedAt.Unix(), n)
	}
	return fmt.Sprintf("ch%d|%d", b.Chapter.ID, n)
}

// PageBounds returns page n's size and content box.
func (s *Service) PageBounds(ctx context.Context, b *BookInfo, n int) (Bounds, error) {
	key := boundsKey(b, n)
	if bd, ok := s.bounds.get(key); ok {
		return bd, nil
	}
	data, _, err := s.Page(ctx, b, n)
	if err != nil {
		return Bounds{}, err
	}
	defer apitiming.Span(ctx, "decode")()
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
	// border kinds: a line where nearly every sample is light (>= 225) or
	// nearly every one is dark (<= 30)
	const (
		none = iota
		light
		dark
	)
	kind := func(samples func(yield func(int))) int {
		n, l, d := 0, 0, 0
		samples(func(v int) {
			n++
			if v >= 225 {
				l++
			} else if v <= 30 {
				d++
			}
		})
		limit := n - max(1, n/100) // allow 1% specks
		switch {
		case l >= limit:
			return light
		case d >= limit:
			return dark
		}
		return none
	}
	row := func(y int) int {
		return kind(func(yield func(int)) {
			for x := 0; x < w; x += stepX {
				yield(luma(x, y))
			}
		})
	}
	col := func(x, y0, y1 int) int {
		return kind(func(yield func(int)) {
			for y := y0; y < y1; y += stepY {
				yield(luma(x, y))
			}
		})
	}
	// each edge trims only the colour it starts with, so a white margin
	// stops at a black frame instead of eating into it
	trim := func(from, to, step int, line func(int) int) int {
		k := line(from)
		if k == none {
			return from
		}
		i := from
		for i != to && line(i) == k {
			i += step
		}
		return i
	}
	top := trim(0, h, 1, row)
	bottom := trim(h-1, top-1, -1, row) + 1
	left := trim(0, w, 1, func(x int) int { return col(x, top, bottom) })
	right := trim(w-1, left-1, -1, func(x int) int { return col(x, top, bottom) }) + 1
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

// PageBoundsMany returns the bounds of several pages at once, so the reader
// asks once per chapter instead of once per page. Pages already measured come
// back immediately; the rest are decoded in parallel until budget runs out,
// and whatever is missing can still be asked for one page at a time.
func (s *Service) PageBoundsMany(ctx context.Context, b *BookInfo, pages []int, budget time.Duration) map[int]Bounds {
	out := make(map[int]Bounds, len(pages))
	var todo []int
	for _, n := range pages {
		if bd, ok := s.bounds.get(boundsKey(b, n)); ok {
			out[n] = bd
		} else {
			todo = append(todo, n)
		}
	}
	if len(todo) == 0 || budget <= 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	workers := max(1, min(runtime.NumCPU()/2, 4)) // decoding is CPU heavy; leave room for the rest
	var mu sync.Mutex
	var wg sync.WaitGroup
	work := make(chan int)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range work {
				bd, err := s.PageBounds(ctx, b, n)
				if err != nil {
					continue // out of budget, or a page that can't be decoded
				}
				mu.Lock()
				out[n] = bd
				mu.Unlock()
			}
		}()
	}
	for _, n := range todo {
		select {
		case work <- n:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(work)
	wg.Wait()
	return out
}
