// Package processing is the download pipeline's processing stage: it
// upscales pages narrower than the profile's minimum width (through an
// upscale module), splits tall strips and then re-encodes pages to save space.
// It runs either before import or later in the background (ProcessBacklog).
package processing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/imageenc"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/upscaling"
)

type Processor struct {
	Up  *upscaling.Processor
	Enc *imageenc.Encoder
	// Guard (optional) pauses encoding when a library server can't read it.
	Guard *Guard
}

func New(up *upscaling.Processor, enc *imageenc.Encoder) *Processor {
	return &Processor{Up: up, Enc: enc}
}

// Unavailable wraps errors that mean a processing engine isn't reachable or
// installed right now (retry later, keep the original meanwhile).
type Unavailable struct{ Err error }

func (u Unavailable) Error() string { return u.Err.Error() }
func (u Unavailable) Unwrap() error { return u.Err }

// Temporary tells the download manager to retry later.
func (u Unavailable) Temporary() bool { return true }

// Process runs the stages enabled in cfg: shrink pages wider than the profile
// allows, upscale the narrow ones, split tall strips, then re-encode. Junk
// images (under the profile's junk size) pass through untouched.
func (p *Processor) Process(ctx context.Context, cfg model.ProfileConfig, pages []downloads.PageFile, workDir string) (downloads.ProcessResult, error) {
	res := downloads.ProcessResult{Pages: pages, SourcePages: make([]int, len(pages))}
	for i := range res.SourcePages {
		res.SourcePages[i] = i
	}
	encoding := cfg.Encode.Format != "" && cfg.Encode.Format != "keep"
	if encoding && p.Guard != nil {
		if blocked, reason := p.Guard.Blocked(); blocked {
			return res, Unavailable{fmt.Errorf("re-encoding is paused: %s", reason)}
		}
	}
	cur := append([]downloads.PageFile(nil), pages...)
	sources := append([]int(nil), res.SourcePages...)
	processable := make([]bool, len(pages))
	var real []int // indexes of the pages that aren't junk
	for i, junk := range junkMask(pages, cfg.Pages) {
		if !junk {
			processable[i] = true
			real = append(real, i)
		}
	}
	pick := func() []downloads.PageFile {
		out := make([]downloads.PageFile, len(real))
		for k, i := range real {
			out[k] = cur[i]
		}
		return out
	}
	put := func(out []downloads.PageFile) {
		for k, i := range real {
			cur[i] = out[k]
		}
	}
	maxWidth := cfg.Pages.MaxWidth
	for _, i := range real {
		if NeedsShrink(cur[i], maxWidth) {
			out, err := shrink(cur[i], maxWidth, encoding, workDir)
			if err != nil {
				return res, err
			}
			cur[i] = out
			res.Shrunk++
			res.Changed = true
		}
	}
	if cfg.Upscale.Enabled && p.Up != nil {
		ucfg := cfg.Upscale
		if maxWidth > 0 {
			ucfg.MaxWidth = maxWidth
		}
		if encoding {
			ucfg.Format = "png" // lossless hand-off to the encoder
		}
		started := time.Now()
		out, applied, mdl, err := p.Up.Process(ctx, ucfg, pick(), workDir)
		if err != nil {
			return res, Unavailable{fmt.Errorf("upscaling: %w", err)}
		}
		res.UpscaleSeconds = time.Since(started).Seconds()
		if applied {
			put(out)
			res.Upscaled, res.UpscaleModel, res.Changed = true, mdl, true
		}
	}
	if cfg.Pages.SplitTall {
		out, mapped, mask, split, err := splitTallPages(ctx, cur, sources, processable, cfg.Pages, encoding, workDir)
		if err != nil {
			return res, err
		}
		if split > 0 {
			cur, sources, processable = out, mapped, mask
			real = real[:0]
			for i, ok := range processable {
				if ok {
					real = append(real, i)
				}
			}
			res.Split, res.Changed = split, true
		}
	}
	if encoding {
		if p.Enc == nil {
			return res, Unavailable{imageenc.ErrNoEngine}
		}
		sel := pick()
		in := make([]imageenc.Page, len(sel))
		for i, pg := range sel {
			in[i] = imageenc.Page{Name: pg.Name, Path: pg.Path, Format: pg.Format, Width: pg.Width, Height: pg.Height}
		}
		started := time.Now()
		out, st, err := p.Enc.EncodePages(ctx, in, cfg.Encode, workDir)
		if err != nil {
			if errors.Is(err, imageenc.ErrNoEngine) {
				return res, Unavailable{err}
			}
			return res, fmt.Errorf("encoding: %w", err)
		}
		res.EncodeSeconds = time.Since(started).Seconds()
		if st.Encoded > 0 {
			enc := make([]downloads.PageFile, len(out))
			for i, pg := range out {
				enc[i] = downloads.PageFile{Name: pg.Name, Path: pg.Path, Format: pg.Format, Width: pg.Width, Height: pg.Height}
			}
			put(enc)
			res.Encoded, res.Encoder, res.Changed = st.Encoded, st.Engine, true
		}
	}
	res.Pages = cur
	res.SourcePages = sources
	res.ProcessedPages = changedPageCount(pages, res.Pages)
	return res, nil
}

func changedPageCount(before, after []downloads.PageFile) int {
	n := min(len(before), len(after))
	changed := max(len(before), len(after)) - n
	for i := 0; i < n; i++ {
		if before[i] != after[i] {
			changed++
		}
	}
	return changed
}

var _ downloads.Processor = (*Processor)(nil)
