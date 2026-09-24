package downloads

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/Asion001/mangarr/internal/model"
)

// screenPages checks freshly downloaded pages against the profile before
// they're processed and imported. A chapter of nothing but junk, one with too
// few real pages, or a low-resolution one the profile won't keep is a
// permanent failure: the release is blocklisted and the next source tried.
// With RemoveJunk the junk images are dropped from the chapter.
func (m *Manager) screenPages(ctx context.Context, jc *jobCtx, pages []PageFile) ([]PageFile, error) {
	cfg := jc.profile.Config
	junk := cfg.Pages.JunkSize()
	var real []PageFile
	for _, p := range pages {
		if !model.IsJunk(p.Width, p.Height, junk) {
			real = append(real, p)
		}
	}
	if len(pages) > 0 && len(real) == 0 {
		return nil, permanent(fmt.Errorf("only junk images: all %d pages are under %d px", len(pages), junk))
	}
	if min := cfg.MinPages; min > 0 && len(real) < min {
		return nil, permanent(fmt.Errorf("chapter has %d real pages, profile requires at least %d", len(real), min))
	}
	if want := cfg.LowRes.MinWidth(); want > 0 {
		if w := medianPageWidth(real); w > 0 && w < want {
			err := fmt.Errorf("low resolution: most pages are %d px wide, the profile wants %d", w, want)
			if cfg.LowRes.Action == model.LowResReject {
				return nil, permanent(err)
			}
			// try another source; keep this one when none is left
			other := false
			if jc.release != nil {
				other, _ = m.searcher.HasAlternative(ctx, jc.series.ID, jc.chapter.ID, jc.release.ID)
			}
			if other {
				return nil, permanent(errors.New(err.Error() + "; trying another source"))
			}
			m.log.Info("low-resolution release kept, no other source has the chapter", "series", jc.series.Title, "chapter", jc.chapter.NumberKey, "width", w)
		}
	}
	if cfg.Pages.RemoveJunk {
		return real, nil
	}
	return pages, nil
}

// medianPageWidth is the median width of the pages, spreads counted per half.
func medianPageWidth(pages []PageFile) int {
	if len(pages) == 0 {
		return 0
	}
	w := make([]int, len(pages))
	for i, p := range pages {
		w[i] = model.PageWidth(p.Width, p.Height)
	}
	slices.Sort(w)
	return w[len(w)/2]
}
