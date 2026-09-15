package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/access"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/reading"
)

func init() { register((*Server).registerRead) }

// ReadChapter is what the web reader needs to show a chapter.
type ReadChapter struct {
	ID          int64  `json:"id"`
	SeriesID    int64  `json:"seriesId"`
	SeriesTitle string `json:"seriesTitle"`
	Number      string `json:"number"`
	Title       string `json:"title,omitempty"`
	Volume      string `json:"volume,omitempty"`
	// ReadingDirection is the series' own (rtl, ltr, vertical, webtoon or empty).
	ReadingDirection string `json:"readingDirection"`
	Downloaded       bool   `json:"downloaded"`
	// CanDownload: the account may download the CBZ.
	CanDownload bool         `json:"canDownload"`
	Pages       []ReadPage   `json:"pages"`
	Prev        *ChapterLink `json:"prev,omitempty"`
	Next        *ChapterLink `json:"next,omitempty"`
	Progress    ReadProgress `json:"progress"`
}

// ReadPage is one page of a chapter.
type ReadPage struct {
	Number    int    `json:"number"`
	MediaType string `json:"mediaType"`
	Size      int64  `json:"size,omitempty"`
}

// ChapterLink points at a neighbouring chapter.
type ChapterLink struct {
	ID     int64  `json:"id"`
	Number string `json:"number"`
	Title  string `json:"title,omitempty"`
}

// ReadProgress is where the reader left off (page is 1-based; 0 = not started).
type ReadProgress struct {
	Page      int  `json:"page"`
	Completed bool `json:"completed"`
}

// ReaderSettingsView is the account's reader settings: defaults and the
// series' own (nil when it has none). The objects belong to the UI.
type ReaderSettingsView struct {
	Defaults json.RawMessage `json:"defaults"`
	Series   json.RawMessage `json:"series,omitempty"`
}

// readerOf is whose progress the web reader shows: the account's, or (API
// key, logins off) the reader apps use.
func (s *Server) readerOf(ctx context.Context) (int64, error) {
	if p := access.From(ctx); p != nil && p.ReaderID > 0 {
		return p.ReaderID, nil
	}
	return s.app.Reading.ReaderID(ctx)
}

func readError(err error) error {
	switch {
	case errors.Is(err, reading.ErrNotFound):
		return huma.Error404NotFound("chapter or page not found")
	case errors.Is(err, reading.ErrNoSource):
		return huma.Error404NotFound(err.Error())
	default:
		return huma.Error502BadGateway("couldn't load the page from the source: " + err.Error())
	}
}

var uaBrowser = regexp.MustCompile(`(Edg|Firefox|Chrome|Safari)/`)

// browserName names the browser for sync health ("Safari on iPad").
func browserName(ua string) string {
	b := "Browser"
	if m := uaBrowser.FindStringSubmatch(ua); m != nil {
		b = map[string]string{"Edg": "Edge"}[m[1]]
		if b == "" {
			b = m[1]
		}
	}
	for _, os := range []string{"iPad", "iPhone", "Android", "Mac OS X", "Windows", "Linux"} {
		if strings.Contains(ua, os) {
			if os == "Mac OS X" {
				os = "macOS"
			}
			return b + " on " + os
		}
	}
	return b
}

func (s *Server) registerRead() {
	tags := []string{"Reader"}

	book := func(ctx context.Context, id int64) (*reading.BookInfo, error) {
		rid, err := s.readerOf(ctx)
		if err != nil {
			return nil, toHTTPError(err)
		}
		b, err := s.app.Reading.Book(ctx, rid, id)
		if err != nil {
			return nil, readError(err)
		}
		return b, nil
	}

	huma.Register(s.api, huma.Operation{OperationID: "read-chapter", Method: http.MethodGet, Path: "/api/v1/read/chapters/{id}", Tags: tags,
		Summary: "A chapter for the web reader: pages, neighbours and your progress"},
		func(ctx context.Context, in *IDPath) (*struct{ Body ReadChapter }, error) {
			rid, err := s.readerOf(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			b, err := s.app.Reading.Book(ctx, rid, in.ID)
			if err != nil {
				return nil, readError(err)
			}
			books, err := s.app.Reading.Books(ctx, rid, b.Chapter.SeriesID)
			if err != nil {
				return nil, toHTTPError(err)
			}
			ser, err := s.app.Series.Get(ctx, b.Chapter.SeriesID)
			if err != nil {
				return nil, seriesError(err)
			}
			pages, err := s.app.Reading.Pages(ctx, b)
			if err != nil {
				return nil, readError(err)
			}
			ch := b.Chapter
			out := ReadChapter{ID: ch.ID, SeriesID: ser.ID, SeriesTitle: ser.Title, Number: ch.NumberKey, Title: ch.Title, Volume: ch.Volume,
				ReadingDirection: ser.ReadingDirection, Downloaded: b.File != nil, CanDownload: b.File != nil && access.From(ctx).Can(access.Download),
				Pages: make([]ReadPage, len(pages))}
			for i, p := range pages {
				out.Pages[i] = ReadPage{Number: p.Number, MediaType: p.MediaType, Size: p.Size}
			}
			for i := range books {
				if books[i].Chapter.ID != ch.ID {
					continue
				}
				if i > 0 {
					c := books[i-1].Chapter
					out.Prev = &ChapterLink{ID: c.ID, Number: c.NumberKey, Title: c.Title}
				}
				if i+1 < len(books) {
					c := books[i+1].Chapter
					out.Next = &ChapterLink{ID: c.ID, Number: c.NumberKey, Title: c.Title}
				}
			}
			if b.State != nil {
				out.Progress = ReadProgress{Page: b.State.Page, Completed: b.State.Completed}
			}
			return &struct{ Body ReadChapter }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "read-page", Method: http.MethodGet, Path: "/api/v1/read/chapters/{id}/pages/{n}", Tags: tags,
		Summary: "A page image (from the file, or streamed from the source)"},
		func(ctx context.Context, in *struct {
			ID int64 `path:"id"`
			N  int   `path:"n" minimum:"1"`
		}) (*imageOutput, error) {
			b, err := book(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			data, ct, err := s.app.Reading.Page(ctx, b, in.N)
			if err != nil {
				return nil, readError(err)
			}
			return &imageOutput{ContentType: ct, CacheControl: "private, max-age=86400", Body: data}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "read-page-bounds", Method: http.MethodGet, Path: "/api/v1/read/chapters/{id}/pages/{n}/bounds", Tags: tags,
		Summary: "A page's size and the box inside its borders (for cropping them)"},
		func(ctx context.Context, in *struct {
			ID int64 `path:"id"`
			N  int   `path:"n" minimum:"1"`
		}) (*struct {
			CacheControl string `header:"Cache-Control"`
			Body         reading.Bounds
		}, error) {
			b, err := book(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			bd, err := s.app.Reading.PageBounds(ctx, b, in.N)
			if err != nil {
				return nil, readError(err)
			}
			return &struct {
				CacheControl string `header:"Cache-Control"`
				Body         reading.Bounds
			}{"private, max-age=86400", bd}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "read-progress", Method: http.MethodPut, Path: "/api/v1/read/chapters/{id}/progress", Tags: tags,
		Summary: "Save where you are in a chapter (the last page finishes it)"},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				Page      int  `json:"page" minimum:"0"`
				Completed bool `json:"completed,omitempty"`
			}
		}) (*struct{}, error) {
			rid, err := s.readerOf(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			ch, pages, err := s.app.Reading.ChapterPages(ctx, in.ID)
			if err != nil {
				return nil, readError(err)
			}
			c := reading.Change{ChapterID: ch.ID, SeriesID: ch.SeriesID, Page: in.Body.Page, Completed: in.Body.Completed}
			if pages > 0 && c.Page >= pages {
				c.Completed = true
			}
			by := reading.By{Origin: model.EventOriginApp, Client: "Web reader", Device: browserName(access.ClientFrom(ctx).UserAgent)}
			if _, err := s.app.Reading.Record(ctx, rid, []reading.Change{c}, by); err != nil {
				return nil, toHTTPError(err)
			}
			return nil, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "read-file", Method: http.MethodGet, Path: "/api/v1/read/chapters/{id}/file", Tags: tags,
		Summary: "Download a downloaded chapter's CBZ"},
		func(ctx context.Context, in *IDPath) (*huma.StreamResponse, error) {
			b, err := book(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			f, st, err := s.app.Reading.File(b)
			if err != nil {
				return nil, huma.Error404NotFound("this chapter isn't downloaded")
			}
			name := filepath.Base(b.Path)
			return &huma.StreamResponse{Body: func(hctx huma.Context) {
				defer f.Close()
				hctx.SetHeader("Content-Type", "application/zip")
				hctx.SetHeader("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
				hctx.SetHeader("Content-Length", strconv.FormatInt(st.Size(), 10))
				_, _ = io.Copy(hctx.BodyWriter(), f)
			}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "read-settings", Method: http.MethodGet, Path: "/api/v1/read/settings", Tags: tags,
		Summary: "Your web reader settings: defaults, and a series' own"},
		func(ctx context.Context, in *struct {
			SeriesID int64 `query:"seriesId"`
		}) (*struct{ Body ReaderSettingsView }, error) {
			p := access.From(ctx)
			out := ReaderSettingsView{Defaults: json.RawMessage("{}")}
			var prefs []model.ReaderPrefs
			if err := s.app.DB.NewSelect().Model(&prefs).Where("user_id = ? AND series_id IN (0, ?)", p.UserID, in.SeriesID).Scan(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			for _, x := range prefs {
				if x.SeriesID == 0 {
					out.Defaults = json.RawMessage(x.Data)
				} else {
					out.Series = json.RawMessage(x.Data)
				}
			}
			return &struct{ Body ReaderSettingsView }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "read-settings-save", Method: http.MethodPut, Path: "/api/v1/read/settings", Tags: tags,
		Summary: "Save reader settings as your defaults (seriesId 0) or for one series (no data: back to the defaults)"},
		func(ctx context.Context, in *struct {
			Body struct {
				SeriesID int64          `json:"seriesId,omitempty"`
				Data     map[string]any `json:"data,omitempty"`
			}
		}) (*struct{}, error) {
			p := access.From(ctx)
			if p.Kind != access.KindUser {
				return nil, huma.Error400BadRequest("sign in as a user to save reader settings")
			}
			if in.Body.Data == nil {
				_, err := s.app.DB.NewDelete().Model((*model.ReaderPrefs)(nil)).Where("user_id = ? AND series_id = ?", p.UserID, in.Body.SeriesID).Exec(ctx)
				return nil, toHTTPError(err)
			}
			data, err := json.Marshal(in.Body.Data)
			if err != nil || len(data) > 8<<10 {
				return nil, huma.Error400BadRequest("settings too large")
			}
			pr := &model.ReaderPrefs{UserID: p.UserID, SeriesID: in.Body.SeriesID, Data: string(data), UpdatedAt: time.Now().UTC()}
			_, err = s.app.DB.NewInsert().Model(pr).On("CONFLICT (user_id, series_id) DO UPDATE").
				Set("data = EXCLUDED.data").Set("updated_at = EXCLUDED.updated_at").Exec(ctx)
			return nil, toHTTPError(err)
		})
}
