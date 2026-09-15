package api

import (
	"context"
	"net/http"

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

func (s *Server) registerReading() {
	tags := []string{"Reading apps"}
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
