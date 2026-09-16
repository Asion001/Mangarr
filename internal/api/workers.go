package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/access"
	"github.com/Asion001/mangarr/internal/auth"
	"github.com/Asion001/mangarr/internal/model"
)

func init() { register((*Server).registerWorkers) }

// WorkerResource is a worker as the UI sees it, with the live state the
// server keeps for it.
type WorkerResource struct {
	model.Worker
	// Online: the worker has been here recently.
	Online bool `json:"online"`
}

// NewWorkerOutput carries the key, which is shown once and never again.
type NewWorkerOutput struct {
	Worker model.Worker `json:"worker"`
	// Key is what the worker container is configured with.
	Key string `json:"key"`
}

// onlineWithin is how long after its last word a worker still counts as
// online (it polls far more often than this).
const onlineWithin = 2 * time.Minute

func (s *Server) registerWorkers() {
	tags := []string{"Workers"}

	huma.Register(s.api, huma.Operation{OperationID: "workers-list", Method: http.MethodGet, Path: "/api/v1/workers", Tags: tags,
		Summary: "List the machines that do work for this server"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []WorkerResource }, error) {
			var list []model.Worker
			if err := s.app.DB.NewSelect().Model(&list).Order("name").Scan(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			out := make([]WorkerResource, 0, len(list))
			for _, w := range list {
				out = append(out, WorkerResource{Worker: w, Online: online(w)})
			}
			return &struct{ Body []WorkerResource }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "workers-create", Method: http.MethodPost, Path: "/api/v1/workers", Tags: tags,
		DefaultStatus: http.StatusCreated, Summary: "Add a worker and issue its key"},
		func(ctx context.Context, in *struct {
			Body struct {
				Name  string   `json:"name" minLength:"1"`
				Roles []string `json:"roles"`
			}
		}) (*struct{ Body NewWorkerOutput }, error) {
			by := int64(0)
			if p := access.From(ctx); p != nil {
				by = p.UserID
			}
			key, w, err := s.app.Auth.CreateWorker(ctx, in.Body.Name, in.Body.Roles, by)
			if err != nil {
				if errors.Is(err, auth.ErrWorkerExists) {
					return nil, huma.Error409Conflict(err.Error())
				}
				return nil, huma.Error422UnprocessableEntity(err.Error())
			}
			return &struct{ Body NewWorkerOutput }{NewWorkerOutput{Worker: *w, Key: key}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "workers-update", Method: http.MethodPut, Path: "/api/v1/workers/{id}", Tags: tags,
		Summary: "Rename a worker, change its roles or switch it off"},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				Name    *string   `json:"name,omitempty"`
				Roles   *[]string `json:"roles,omitempty"`
				Enabled *bool     `json:"enabled,omitempty"`
			}
		}) (*struct{ Body model.Worker }, error) {
			var w model.Worker
			if err := s.app.DB.NewSelect().Model(&w).Where("id = ?", in.ID).Scan(ctx); err != nil {
				return nil, huma.Error404NotFound("no such worker")
			}
			if in.Body.Name != nil {
				name := strings.TrimSpace(*in.Body.Name)
				if name == "" {
					return nil, huma.Error422UnprocessableEntity("a worker needs a name")
				}
				if n, _ := s.app.DB.NewSelect().Model((*model.Worker)(nil)).Where("name = ? AND id <> ?", name, in.ID).Count(ctx); n > 0 {
					return nil, huma.Error409Conflict(auth.ErrWorkerExists.Error())
				}
				w.Name = name
			}
			if in.Body.Roles != nil {
				kept := []string{}
				for _, r := range model.WorkerRoles {
					for _, want := range *in.Body.Roles {
						if r == want {
							kept = append(kept, r)
						}
					}
				}
				if len(kept) == 0 {
					return nil, huma.Error422UnprocessableEntity("a worker needs at least one role")
				}
				w.Roles = kept
			}
			if in.Body.Enabled != nil {
				w.Enabled = *in.Body.Enabled
			}
			if _, err := s.app.DB.NewUpdate().Model(&w).Column("name", "roles", "enabled").WherePK().Exec(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			s.app.Auth.InvalidateWorkers()
			return &struct{ Body model.Worker }{w}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "workers-delete", Method: http.MethodDelete, Path: "/api/v1/workers/{id}", Tags: tags,
		DefaultStatus: http.StatusNoContent, Summary: "Remove a worker and its key"},
		func(ctx context.Context, in *struct {
			ID int64 `path:"id"`
		}) (*struct{}, error) {
			res, err := s.app.DB.NewDelete().Model((*model.Worker)(nil)).Where("id = ?", in.ID).Exec(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, huma.Error404NotFound("no such worker")
			}
			s.app.Auth.InvalidateWorkers()
			return &struct{}{}, nil
		})
}

func online(w model.Worker) bool {
	return w.Enabled && w.LastSeenAt != nil && time.Since(*w.LastSeenAt) < onlineWithin
}
