package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/cbz"
	"github.com/Asion001/mangarr/internal/downloads"
	"github.com/Asion001/mangarr/internal/imagecheck"
	"github.com/Asion001/mangarr/internal/imageenc"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/processing"
	"github.com/Asion001/mangarr/internal/settings"
)

func init() { register((*Server).registerProcessing) }

type EngineInfo struct {
	Name   string `json:"name"`
	Format string `json:"format"`
	Slow   bool   `json:"slow"`
}

type ProcessingStatus struct {
	Engines []EngineInfo     `json:"engines"`
	State   processing.State `json:"state"`
	// Pending counts files waiting for background processing; Failed gave up.
	Pending    int   `json:"pending"`
	Failed     int   `json:"failed"`
	Processed  int   `json:"processed"`
	SpaceSaved int64 `json:"spaceSaved"`
}

type PreviewPage struct {
	Index          int    `json:"index"`
	Name           string `json:"name"`
	Width          int    `json:"width"`
	Height         int    `json:"height"`
	OriginalFormat string `json:"originalFormat"`
	OriginalSize   int64  `json:"originalSize"`
	EncodedFormat  string `json:"encodedFormat"`
	EncodedSize    int64  `json:"encodedSize"`
}

type PreviewResult struct {
	Token   string        `json:"token"`
	Engine  string        `json:"engine"`
	Seconds float64       `json:"seconds"`
	Pages   []PreviewPage `json:"pages"`
}

type preview struct {
	dir     string
	created time.Time
	orig    []string
	enc     []string
}

var (
	previewMu sync.Mutex
	previews  = map[string]*preview{}
)

func (s *Server) cleanPreviews() {
	previewMu.Lock()
	defer previewMu.Unlock()
	for k, p := range previews {
		if time.Since(p.created) > time.Hour {
			os.RemoveAll(p.dir)
			delete(previews, k)
		}
	}
}

func (s *Server) registerProcessing() {
	tags := []string{"Processing"}
	huma.Register(s.api, huma.Operation{OperationID: "processing-status", Method: http.MethodGet, Path: "/api/v1/processing", Tags: tags,
		Summary: "Encoders, background backlog and space saved"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body ProcessingStatus }, error) {
			st := ProcessingStatus{Engines: []EngineInfo{}}
			for _, e := range s.app.Encoder.Engines() {
				st.Engines = append(st.Engines, EngineInfo{Name: e.Name(), Format: e.Format(), Slow: e.Slow()})
			}
			st.State = s.app.Processing.Guard.State(ctx)
			var profiles []model.Profile
			_ = s.app.DB.NewSelect().Model(&profiles).Scan(ctx)
			for _, p := range profiles {
				params := p.Config.ProcessParams()
				if params == "" {
					continue
				}
				base := s.app.DB.NewSelect().Model((*model.ChapterFile)(nil)).Where("process_params <> ?", params).
					Where("series_id IN (SELECT id FROM series WHERE profile_id = ?)", p.ID)
				n, _ := base.Where("process_attempts < ?", downloads.MaxProcessAttempts).Count(ctx)
				st.Pending += n
				f, _ := s.app.DB.NewSelect().Model((*model.ChapterFile)(nil)).Where("process_params <> ?", params).
					Where("series_id IN (SELECT id FROM series WHERE profile_id = ?)", p.ID).
					Where("process_attempts >= ?", downloads.MaxProcessAttempts).Count(ctx)
				st.Failed += f
			}
			var agg struct {
				Processed int   `bun:"processed"`
				Saved     int64 `bun:"saved"`
			}
			_ = s.app.DB.NewSelect().Model((*model.ChapterFile)(nil)).
				ColumnExpr("SUM(CASE WHEN process_state = ? THEN 1 ELSE 0 END) AS processed", model.ProcessDone).
				ColumnExpr("COALESCE(SUM(CASE WHEN size_original > size THEN size_original - size ELSE 0 END), 0) AS saved").Scan(ctx, &agg)
			st.Processed, st.SpaceSaved = agg.Processed, agg.Saved
			return &struct{ Body ProcessingStatus }{st}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "processing-resume", Method: http.MethodPost, Path: "/api/v1/processing/resume", Tags: tags,
		Summary: "Resume re-encoding after it was paused because a library server couldn't read the files"},
		func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			if err := s.app.Processing.Guard.Resume(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			s.app.PushProcessBacklog("resumed")
			return nil, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "processing-preview", Method: http.MethodPost, Path: "/api/v1/processing/preview", Tags: tags,
		Summary: "Re-encode three pages of a chapter with the given settings to compare quality and size"},
		func(ctx context.Context, in *struct {
			Body struct {
				ChapterID int64              `json:"chapterId"`
				Encode    model.EncodeConfig `json:"encode"`
			}
		}) (*struct{ Body PreviewResult }, error) {
			s.cleanPreviews()
			var f model.ChapterFile
			if err := s.app.DB.NewSelect().Model(&f).Where("chapter_id = ?", in.Body.ChapterID).Scan(ctx); err != nil {
				return nil, huma.Error404NotFound("chapter has no file")
			}
			var ser model.Series
			if err := s.app.DB.NewSelect().Model(&ser).Where("id = ?", f.SeriesID).Scan(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			dir, err := s.app.Library.SeriesDir(ctx, &ser)
			if err != nil {
				return nil, toHTTPError(err)
			}
			pages, _, err := cbz.Read(filepath.Join(dir, f.RelativePath))
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("read chapter: " + err.Error())
			}
			if len(pages) == 0 {
				return nil, huma.Error422UnprocessableEntity("chapter has no pages")
			}
			token := settings.RandomHex(8)
			work := filepath.Join(s.app.Cfg.DataDir, "preview", token)
			if err := os.MkdirAll(work, 0o775); err != nil {
				return nil, toHTTPError(err)
			}
			// first page after the cover, the middle one and a late one
			picks := []int{min(1, len(pages)-1), len(pages) / 2, max(len(pages)-2, 0)}
			var in2 []imageenc.Page
			seen := map[int]bool{}
			for _, i := range picks {
				if seen[i] {
					continue
				}
				seen[i] = true
				info, err := imagecheck.Detect(pages[i].Data)
				if err != nil {
					continue
				}
				p := filepath.Join(work, pages[i].Name)
				if err := os.WriteFile(p, pages[i].Data, 0o664); err != nil {
					return nil, toHTTPError(err)
				}
				in2 = append(in2, imageenc.Page{Name: pages[i].Name, Path: p, Format: info.Format, Width: info.Width, Height: info.Height})
			}
			cfg := in.Body.Encode
			cfg.MinSavingsPct = -1000 // always show the encoded page
			start := time.Now()
			out, st, err := s.app.Encoder.EncodePages(ctx, in2, cfg, work)
			if err != nil {
				os.RemoveAll(work)
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			res := PreviewResult{Token: token, Engine: st.Engine, Seconds: time.Since(start).Seconds(), Pages: []PreviewPage{}}
			pv := &preview{dir: work, created: time.Now()}
			for i := range in2 {
				pp := PreviewPage{Index: i, Name: in2[i].Name, Width: in2[i].Width, Height: in2[i].Height, OriginalFormat: in2[i].Format,
					EncodedFormat: out[i].Format}
				if fi, err := os.Stat(in2[i].Path); err == nil {
					pp.OriginalSize = fi.Size()
				}
				if fi, err := os.Stat(out[i].Path); err == nil {
					pp.EncodedSize = fi.Size()
				}
				pv.orig, pv.enc = append(pv.orig, in2[i].Path), append(pv.enc, out[i].Path)
				res.Pages = append(res.Pages, pp)
			}
			previewMu.Lock()
			previews[token] = pv
			previewMu.Unlock()
			return &struct{ Body PreviewResult }{res}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "processing-preview-image", Method: http.MethodGet, Path: "/api/v1/processing/preview/{token}/{index}/{variant}", Tags: tags},
		func(ctx context.Context, in *struct {
			Token   string `path:"token"`
			Index   int    `path:"index"`
			Variant string `path:"variant" enum:"original,encoded"`
		}) (*imageOutput, error) {
			previewMu.Lock()
			pv := previews[in.Token]
			previewMu.Unlock()
			if pv == nil || in.Index < 0 || in.Index >= len(pv.orig) {
				return nil, huma.Error404NotFound("preview expired")
			}
			p := pv.orig[in.Index]
			if in.Variant == "encoded" {
				p = pv.enc[in.Index]
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil, huma.Error404NotFound("preview expired")
			}
			info, _ := imagecheck.Detect(data)
			return &imageOutput{ContentType: "image/" + info.Format, CacheControl: "private, max-age=3600", Body: data}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "processing-preview-cbz", Method: http.MethodGet, Path: "/api/v1/processing/preview/{token}/sample.cbz", Tags: tags,
		Summary: "The encoded preview pages as a CBZ, to check in a reader app"},
		func(ctx context.Context, in *struct {
			Token string `path:"token"`
		}) (*struct {
			ContentType        string `header:"Content-Type"`
			ContentDisposition string `header:"Content-Disposition"`
			Body               []byte
		}, error) {
			previewMu.Lock()
			pv := previews[in.Token]
			previewMu.Unlock()
			if pv == nil {
				return nil, huma.Error404NotFound("preview expired")
			}
			var pages []cbz.Page
			for i, p := range pv.enc {
				pages = append(pages, cbz.Page{Name: cbz.PageName(i, filepath.Ext(p)), Path: p})
			}
			dst := filepath.Join(pv.dir, "sample.cbz")
			if _, err := cbz.Write(dst, pages, nil, 0, time.Now()); err != nil {
				return nil, toHTTPError(err)
			}
			data, err := os.ReadFile(dst)
			if err != nil {
				return nil, toHTTPError(err)
			}
			return &struct {
				ContentType        string `header:"Content-Type"`
				ContentDisposition string `header:"Content-Disposition"`
				Body               []byte
			}{"application/vnd.comicbook+zip", fmt.Sprintf(`attachment; filename="mangarr-preview-%s.cbz"`, in.Token), data}, nil
		})
}
