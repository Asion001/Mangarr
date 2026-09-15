package api

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/access"
	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/modules"
)

func init() { register((*Server).registerMyNotifications) }

// maxOwnTargets caps a user's notification targets.
const maxOwnTargets = 10

// OwnTargetInput is one of your notification targets.
type OwnTargetInput struct {
	Implementation string         `json:"implementation"`
	Name           string         `json:"name"`
	Enabled        bool           `json:"enabled"`
	Events         []string       `json:"events,omitempty"`
	Settings       map[string]any `json:"settings"`
}

func (in OwnTargetInput) def(id, userID int64) model.ProviderDefinition {
	var evs []string
	for _, e := range in.Events {
		if slices.Contains(events.PersonalEvents, e) {
			evs = append(evs, e)
		}
	}
	return model.ProviderDefinition{ID: id, Kind: string(modules.KindNotify), Implementation: in.Implementation, Name: in.Name,
		Enabled: in.Enabled, Priority: 25, Events: evs, Settings: in.Settings, UserID: &userID}
}

// me is the signed-in user (not the API key or logins off).
func me(ctx context.Context) (*access.Principal, error) {
	p := access.From(ctx)
	if p == nil || p.Kind != access.KindUser {
		return nil, huma.Error400BadRequest("sign in as a user to have your own notifications")
	}
	return p, nil
}

// ownTarget loads one of the caller's targets.
func (s *Server) ownTarget(ctx context.Context, id int64) (*modules.Loaded, error) {
	p, err := me(ctx)
	if err != nil {
		return nil, err
	}
	l, ok := s.app.Modules.Get(id)
	if !ok || l.Def.UserID == nil || *l.Def.UserID != p.UserID {
		return nil, huma.Error404NotFound("notification target not found")
	}
	return l, nil
}

func (s *Server) registerMyNotifications() {
	tags := []string{"Account"}

	huma.Register(s.api, huma.Operation{OperationID: "me-notifications-schema", Method: http.MethodGet, Path: "/api/v1/me/notifications/schema", Tags: tags,
		Summary: "Notification services you can send your own notifications to"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []ImplementationResource }, error) {
			out := []ImplementationResource{}
			for _, impl := range modules.Implementations(modules.KindNotify) {
				out = append(out, ImplementationResource{Kind: string(modules.KindNotify), Name: impl.Name, DisplayName: impl.DisplayName,
					Description: impl.Description, InfoURL: impl.InfoURL, Fields: modules.FieldsOf(impl.Settings()), Events: events.PersonalEvents})
			}
			return &struct{ Body []ImplementationResource }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "me-notifications", Method: http.MethodGet, Path: "/api/v1/me/notifications", Tags: tags,
		Summary: "Your notification targets"},
		func(ctx context.Context, _ *struct{}) (*struct{ Body []ModuleResource }, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			out := []ModuleResource{}
			for _, l := range s.app.Modules.All(modules.KindNotify) {
				if l.Def.UserID != nil && *l.Def.UserID == p.UserID {
					out = append(out, s.toModuleResource(l))
				}
			}
			return &struct{ Body []ModuleResource }{out}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "me-notifications-create", Method: http.MethodPost, Path: "/api/v1/me/notifications", Tags: tags},
		func(ctx context.Context, in *struct{ Body OwnTargetInput }) (*struct{ Body ModuleResource }, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			n := 0
			for _, l := range s.app.Modules.All(modules.KindNotify) {
				if l.Def.UserID != nil && *l.Def.UserID == p.UserID {
					n++
				}
			}
			if n >= maxOwnTargets {
				return nil, huma.Error400BadRequest("you have the most notification targets you can have")
			}
			def := in.Body.def(0, p.UserID)
			if err := s.app.Modules.Create(ctx, &def); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			l, _ := s.app.Modules.Get(def.ID)
			return &struct{ Body ModuleResource }{s.toModuleResource(l)}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "me-notifications-update", Method: http.MethodPut, Path: "/api/v1/me/notifications/{id}", Tags: tags},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body OwnTargetInput
		}) (*struct{ Body ModuleResource }, error) {
			l, err := s.ownTarget(ctx, in.ID)
			if err != nil {
				return nil, err
			}
			def := in.Body.def(in.ID, *l.Def.UserID)
			if err := s.app.Modules.Update(ctx, &def); err != nil {
				if errors.Is(err, modules.ErrNotFound) {
					return nil, huma.Error404NotFound("notification target not found")
				}
				return nil, huma.Error400BadRequest(err.Error())
			}
			l, _ = s.app.Modules.Get(def.ID)
			return &struct{ Body ModuleResource }{s.toModuleResource(l)}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "me-notifications-delete", Method: http.MethodDelete, Path: "/api/v1/me/notifications/{id}", Tags: tags},
		func(ctx context.Context, in *IDPath) (*struct{}, error) {
			if _, err := s.ownTarget(ctx, in.ID); err != nil {
				return nil, err
			}
			return nil, toHTTPError(s.app.Modules.Delete(ctx, in.ID))
		})

	huma.Register(s.api, huma.Operation{OperationID: "me-notifications-test", Method: http.MethodPost, Path: "/api/v1/me/notifications/test", Tags: tags,
		Summary: "Send a test message to a target (saved or not)"},
		func(ctx context.Context, in *struct {
			Body struct {
				ID int64 `json:"id,omitempty"`
				OwnTargetInput
			}
		}) (*struct{}, error) {
			p, err := me(ctx)
			if err != nil {
				return nil, err
			}
			if in.Body.ID != 0 {
				if _, err := s.ownTarget(ctx, in.Body.ID); err != nil {
					return nil, err
				}
			}
			if err := s.app.Modules.TestDefinition(ctx, in.Body.def(in.Body.ID, p.UserID)); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return nil, nil
		})
}
