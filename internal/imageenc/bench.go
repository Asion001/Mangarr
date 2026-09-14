package imageenc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/Asion001/mangarr/internal/cbz"
	"github.com/Asion001/mangarr/internal/imagecheck"
	"github.com/Asion001/mangarr/internal/model"
)

// Bench re-encodes up to maxPages pages of a CBZ with every preset of the
// given formats and prints sizes and timings, to pick settings for your
// hardware and pages.
func (e *Encoder) Bench(ctx context.Context, cbzPath string, formats []string, maxPages int, w io.Writer) error {
	pages, _, err := cbz.Read(cbzPath)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "mangarr-bench-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var in []Page
	var total int64
	for i, p := range pages {
		if maxPages > 0 && len(in) >= maxPages {
			break
		}
		info, err := imagecheck.Detect(p.Data)
		if err != nil {
			continue
		}
		path := filepath.Join(tmp, cbz.PageName(i, imagecheck.Ext(info.Format)))
		if err := os.WriteFile(path, p.Data, 0o644); err != nil {
			return err
		}
		in = append(in, Page{Name: filepath.Base(path), Path: path, Format: info.Format, Width: info.Width, Height: info.Height})
		total += int64(len(p.Data))
	}
	if len(in) == 0 {
		return fmt.Errorf("no readable pages in %s", cbzPath)
	}
	fmt.Fprintf(w, "%d pages, %.1f MB, %d threads\n\n", len(in), float64(total)/(1<<20), e.Threads)
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "FORMAT\tPRESET\tENGINE\tENCODED\tSIZE\tSAVED\tSEC/PAGE")
	for _, f := range formats {
		for _, preset := range []string{"fast", "balanced", "max"} {
			cfg := model.EncodeConfig{Format: f, Preset: preset, Grayscale: true, MinSavingsPct: -1000}
			start := time.Now()
			out, st, err := e.EncodePages(ctx, in, cfg, filepath.Join(tmp, f+"-"+preset))
			if err != nil {
				fmt.Fprintf(tw, "%s\t%s\t-\t-\t-\t-\t%v\n", f, preset, err)
				continue
			}
			var size int64
			for _, p := range out {
				if fi, err := os.Stat(p.Path); err == nil {
					size += fi.Size()
				}
			}
			secs := time.Since(start).Seconds() / float64(max(st.Encoded, 1))
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d/%d\t%.1f MB\t%.0f%%\t%.1f\n", f, preset, st.Engine, st.Encoded, len(in),
				float64(size)/(1<<20), 100*(1-float64(size)/float64(total)), secs)
		}
	}
	fmt.Fprintln(tw, "\nSEC/PAGE is wall-clock time per page with all threads busy; sizes include pages that weren't encoded.")
	return tw.Flush()
}
