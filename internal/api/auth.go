package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/auth"
)

type AuthStatus struct {
	NeedsSetup    bool   `json:"needsSetup"`
	Authenticated bool   `json:"authenticated"`
	User          string `json:"user,omitempty"`
	AuthDisabled  bool   `json:"authDisabled"`
}

type Credentials struct {
	Username string `json:"username" minLength:"1"`
	Password string `json:"password" minLength:"1"`
}

type loginOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      AuthStatus
}

func (s *Server) registerAuth() {
	tags := []string{"Auth"}
	huma.Register(s.api, huma.Operation{OperationID: "auth-status", Method: http.MethodGet, Path: "/api/v1/auth/status", Tags: tags, Security: []map[string][]string{}},
		func(ctx context.Context, _ *struct{}) (*struct{ Body AuthStatus }, error) {
			needs, err := s.app.Auth.NeedsSetup(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			p := auth.Principal(ctx)
			return &struct{ Body AuthStatus }{AuthStatus{NeedsSetup: needs && !s.app.Auth.Disabled(), Authenticated: p != "", User: p, AuthDisabled: s.app.Auth.Disabled()}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "auth-setup", Method: http.MethodPost, Path: "/api/v1/auth/setup", Tags: tags, Security: []map[string][]string{},
		Summary: "Create the first user (only allowed when no user exists)"},
		func(ctx context.Context, in *struct{ Body Credentials }) (*loginOutput, error) {
			needs, err := s.app.Auth.NeedsSetup(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			if !needs {
				return nil, huma.Error409Conflict("setup already completed")
			}
			if _, err := s.app.Auth.CreateUser(ctx, in.Body.Username, in.Body.Password); err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			return s.login(ctx, in.Body.Username)
		})

	huma.Register(s.api, huma.Operation{OperationID: "auth-login", Method: http.MethodPost, Path: "/api/v1/auth/login", Tags: tags, Security: []map[string][]string{}},
		func(ctx context.Context, in *struct{ Body Credentials }) (*loginOutput, error) {
			if _, err := s.app.Auth.Verify(ctx, in.Body.Username, in.Body.Password); err != nil {
				return nil, huma.Error401Unauthorized(err.Error())
			}
			return s.login(ctx, in.Body.Username)
		})

	huma.Register(s.api, huma.Operation{OperationID: "auth-logout", Method: http.MethodPost, Path: "/api/v1/auth/logout", Tags: tags},
		func(ctx context.Context, _ *struct{}) (*struct {
			SetCookie http.Cookie `header:"Set-Cookie"`
		}, error) {
			return &struct {
				SetCookie http.Cookie `header:"Set-Cookie"`
			}{http.Cookie{Name: auth.CookieName, Value: "", Path: "/", MaxAge: -1}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "auth-password", Method: http.MethodPost, Path: "/api/v1/auth/password", Tags: tags},
		func(ctx context.Context, in *struct {
			Body struct {
				Password string `json:"password" minLength:"6"`
			}
		}) (*struct{}, error) {
			user := auth.Principal(ctx)
			if user == "apikey" || user == "anonymous" {
				return nil, huma.Error400BadRequest("log in as a user to change the password")
			}
			return nil, toHTTPError(s.app.Auth.ChangePassword(ctx, user, in.Body.Password))
		})
}

func (s *Server) login(ctx context.Context, username string) (*loginOutput, error) {
	c, err := s.app.Auth.SessionCookie(ctx, username, false)
	if err != nil {
		return nil, toHTTPError(err)
	}
	return &loginOutput{SetCookie: *c, Body: AuthStatus{Authenticated: true, User: username}}, nil
}
