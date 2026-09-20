// Package processing is the download pipeline's processing stage: it
// upscales pages narrower than the profile's minimum width (through an
// upscale module) and then re-encodes pages to save space (AVIF / JPEG XL).
// It runs either before import or later in the background (ProcessBacklog).
package processing

import (
	"context"
	"errors"
	"fmt"

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

// Process runs the stages enabled in cfg.
func (p *Processor) Process(ctx context.Context, cfg model.ProfileConfig, pages []downloads.PageFile, workDir string) (downloads.ProcessResult, error) {
	res := downloads.ProcessResult{Pages: pages}
	encoding := cfg.Encode.Format != "" && cfg.Encode.Format != "keep"
	if encoding && p.Guard != nil {
		if blocked, reason := p.Guard.Blocked(); blocked {
			return res, Unavailable{fmt.Errorf("re-encoding is paused: %s", reason)}
		}
	}
	if cfg.Upscale.Enabled && p.Up != nil {
		ucfg := cfg.Upscale
		if encoding {
			ucfg.Format = "png" // lossless hand-off to the encoder
		}
		out, applied, mdl, err := p.Up.Process(ctx, ucfg, pages, workDir)
		if err != nil {
			return res, Unavailable{fmt.Errorf("upscaling: %w", err)}
		}
		if applied {
			res.Pages, res.Upscaled, res.UpscaleModel, res.Changed = out, true, mdl, true
		}
	}
	if encoding {
		if p.Enc == nil {
			return res, Unavailable{imageenc.ErrNoEngine}
		}
		in := make([]imageenc.Page, len(res.Pages))
		for i, pg := range res.Pages {
			in[i] = imageenc.Page{Name: pg.Name, Path: pg.Path, Format: pg.Format, Width: pg.Width, Height: pg.Height}
		}
		out, st, err := p.Enc.EncodePages(ctx, in, cfg.Encode, workDir)
		if err != nil {
			if errors.Is(err, imageenc.ErrNoEngine) {
				return res, Unavailable{err}
			}
			return res, fmt.Errorf("encoding: %w", err)
		}
		if st.Encoded > 0 {
			pages := make([]downloads.PageFile, len(out))
			for i, pg := range out {
				pages[i] = downloads.PageFile{Name: pg.Name, Path: pg.Path, Format: pg.Format, Width: pg.Width, Height: pg.Height}
			}
			res.Pages, res.Encoded, res.Encoder, res.Changed = pages, st.Encoded, st.Engine, true
		}
	}
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
