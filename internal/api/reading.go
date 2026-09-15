package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/komgaapi"
	"github.com/Asion001/mangarr/internal/model"
)

func init() { register((*Server).registerReading) }

// NewReadingKey is a key as created: Key is only returned this once.
type NewReadingKey struct {
	model.ReadingKey
	Key string `json:"key"`
}

// ShelfItem is a series on the "Continue reading" shelf.
type ShelfItem struct {
	SeriesID int64  `json:"seriesId"`
	Title    string `json:"title"`
	CoverURL string `json:"coverUrl"`
	// Next is the chapter to read next; Page is where the reader left off
	// in it (0 = not started).
	Next       NextChapter `json:"next"`
	Page       int         `json:"page"`
	Read       int         `json:"read"`
	Total      int         `json:"total"`
	LastReadAt *time.Time  `json:"lastReadAt,omitempty"`
}

// Shelf is a reader's "Continue reading" shelf.
type Shelf struct {
	ReaderID int64       `json:"readerId"`
	Reader   string      `json:"reader"`
	Items    []ShelfItem `json:"items"`
}

func (s *Server) registerReading() {
	tags := []string{"Reading apps"}
	huma.Register(s.api, huma.Operation{OperationID: "reading-shelf", Method: http.MethodGet, Path: "/api/v1/reading/shelf", Tags: tags,
		Summary: "Continue reading: the next chapter of each series the reader started, most recently read first"},
		func(ctx context.Context, in *struct {
			ReaderID int64 `query:"readerId" doc:"Reader (0 = the one reading apps act as)"`
			Limit    int   `query:"limit" default:"20" minimum:"1" maximum:"100"`
		}) (*struct{ Body Shelf }, error) {
			rid := in.ReaderID
			if rid == 0 {
				// (without readers there's no shelf; don't create one)
				if n, err := s.app.DB.NewSelect().Model((*model.Reader)(nil)).Count(ctx); err != nil || n == 0 {
					return &struct{ Body Shelf }{Shelf{Items: []ShelfItem{}}}, toHTTPError(err)
				}
				var err error
				if rid, err = s.app.Reading.ReaderID(ctx); err != nil {
					return nil, toHTTPError(err)
				}
			}
			var r model.Reader
			if err := s.app.DB.NewSelect().Model(&r).Where("id = ?", rid).Scan(ctx); err != nil {
				return nil, huma.Error404NotFound("reader not found")
			}
			next, err := s.app.Reading.OnDeck(ctx, rid)
			if err != nil {
				return nil, toHTTPError(err)
			}
			out := Shelf{ReaderID: rid, Reader: r.Name, Items: []ShelfItem{}}
			for _, n := range next[:min(in.Limit, len(next))] {
				ser, ch := n.Series.Series, n.Book.Chapter
				it := ShelfItem{SeriesID: ser.ID, Title: ser.Title,
					CoverURL: "api/v1/series/" + strconv.FormatInt(ser.ID, 10) + "/cover?v=" + strconv.FormatInt(ser.UpdatedAt.Unix(), 10),
					Next:     NextChapter{ChapterID: ch.ID, Number: ch.NumberKey, Title: ch.Title, Available: n.Book.File != nil},
					Read:     n.Series.Read, Total: n.Series.Books, LastReadAt: n.Series.LastRead}
				if n.Book.State != nil && !n.Book.State.Completed {
					it.Page = n.Book.State.Page
				}
				out.Items = append(out.Items, it)
			}
			return &struct{ Body Shelf }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "reading-status", Method: http.MethodGet, Path: "/api/v1/reading/status", Tags: tags,
		Summary: "Whether the Komga-compatible API is enabled and listening"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body komgaapi.Status }, error) {
			return &struct{ Body komgaapi.Status }{s.app.Komga.Status(ctx)}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "reading-keys", Method: http.MethodGet, Path: "/api/v1/reading/keys", Tags: tags,
		Summary: "API keys of reading apps (one per device)"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []model.ReadingKey }, error) {
			out := []model.ReadingKey{}
			err := s.app.DB.NewSelect().Model(&out).Order("id").Scan(ctx)
			return &struct{ Body []model.ReadingKey }{out}, toHTTPError(err)
		})

	huma.Register(s.api, huma.Operation{OperationID: "reading-keys-create", Method: http.MethodPost, Path: "/api/v1/reading/keys", Tags: tags,
		Summary: "Create a key for a reading app; the key is only shown in this response"},
		func(ctx context.Context, in *struct {
			Body struct {
				Comment string `json:"comment" doc:"Device name, e.g. \"Mihon phone\""`
			}
		}) (*struct{ Body NewReadingKey }, error) {
			key, rk, err := s.app.Komga.CreateKey(ctx, in.Body.Comment, "")
			if err != nil {
				return nil, toHTTPError(err)
			}
			s.app.Bus.Changed("reading", "key-created", rk.ID)
			return &struct{ Body NewReadingKey }{NewReadingKey{ReadingKey: *rk, Key: key}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "reading-keys-delete", Method: http.MethodDelete, Path: "/api/v1/reading/keys/{id}", Tags: tags},
		func(ctx context.Context, in *IDPath) (*struct{}, error) {
			if _, err := s.app.DB.NewDelete().Model((*model.ReadingKey)(nil)).Where("id = ?", in.ID).Exec(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			s.app.Komga.InvalidateKeys()
			s.app.Bus.Changed("reading", "key-deleted", in.ID)
			return nil, nil
		})
}
