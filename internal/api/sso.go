package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/access"
	"github.com/Asion001/mangarr/internal/settings"
	"github.com/Asion001/mangarr/internal/sso"
)

func init() { register((*Server).registerSSO) }

// secretMask stands in for a stored secret the UI never sees.
const secretMask = "••••••••"

// SSOSettings is the single sign-on configuration (the client secret is masked).
type SSOSettings struct {
	settings.SSO
	// RedirectURL is what the provider must allow (register it there).
	RedirectURL string `json:"redirectUrl" readOnly:"true"`
}

// redirectURL is where the provider sends people back, from the public URL
// when it's set (else the address the browser used).
func (s *Server) redirectURL(ctx context.Context) string {
	base := ""
	if g, err := s.app.Settings.General(ctx); err == nil {
		base = strings.TrimRight(g.PublicURL, "/")
	}
	if base == "" {
		c := access.ClientFrom(ctx)
		scheme := "http"
		if c.Secure {
			scheme = "https"
		}
		if c.Host == "" {
			return ""
		}
		base = scheme + "://" + c.Host
	}
	return base + s.app.Cfg.URLBase + "/api/v1/auth/oidc/callback"
}

// ssoError sends people back to the login page with the reason.
func (s *Server) ssoError(ctx context.Context, err error) *ssoRedirect {
	s.app.Log.Info("single sign-on failed", "err", err)
	msg := err.Error()
	if errors.Is(err, sso.ErrDisabled) {
		msg = "single sign-on is off"
	}
	return &ssoRedirect{Status: http.StatusSeeOther, Location: s.app.Cfg.URLBase + "/login?error=" + url.QueryEscape(msg),
		SetCookie: http.Cookie{Name: sso.StateCookie, Value: "", Path: "/", MaxAge: -1}}
}

type ssoRedirect struct {
	Status    int
	Location  string      `header:"Location"`
	SetCookie http.Cookie `header:"Set-Cookie"`
}

func (s *Server) registerSSO() {
	tags := []string{"Auth"}
	public := []map[string][]string{}

	huma.Register(s.api, huma.Operation{OperationID: "auth-oidc-login", Method: http.MethodGet, Path: "/api/v1/auth/oidc/login", Tags: tags, Security: public,
		Summary: "Start signing in with the single sign-on provider (redirects there)"},
		func(ctx context.Context, in *struct {
			Invite string `query:"invite" doc:"Invite token to redeem with the provider's account"`
			Next   string `query:"next" doc:"Where to go afterwards (a path on this server)"`
		}) (*ssoRedirect, error) {
			redirect := s.redirectURL(ctx)
			if redirect == "" {
				return s.ssoError(ctx, errors.New("set the public URL under Settings → General first")), nil
			}
			u, cookie, err := s.app.SSO.Begin(ctx, redirect, in.Invite, in.Next)
			if err != nil {
				return s.ssoError(ctx, err), nil
			}
			c := access.ClientFrom(ctx)
			return &ssoRedirect{Status: http.StatusSeeOther, Location: u,
				SetCookie: http.Cookie{Name: sso.StateCookie, Value: cookie, Path: "/", HttpOnly: true, Secure: c.Secure,
					SameSite: http.SameSiteLaxMode, MaxAge: 600}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "auth-oidc-callback", Method: http.MethodGet, Path: "/api/v1/auth/oidc/callback", Tags: tags, Security: public,
		Summary: "Where the single sign-on provider sends people back"},
		func(ctx context.Context, in *struct {
			Code             string `query:"code"`
			State            string `query:"state"`
			Error            string `query:"error"`
			ErrorDescription string `query:"error_description"`
			Cookie           string `cookie:"mangarr_oidc"`
		}) (*ssoRedirect, error) {
			q := url.Values{"code": {in.Code}, "state": {in.State}}
			if in.Error != "" {
				q.Set("error", in.Error)
				q.Set("error_description", in.ErrorDescription)
			}
			u, next, err := s.app.SSO.Finish(ctx, s.redirectURL(ctx), q, in.Cookie)
			if err != nil {
				return s.ssoError(ctx, err), nil
			}
			cookie, err := s.app.Auth.StartSession(ctx, u.ID, access.ClientFrom(ctx))
			if err != nil {
				return s.ssoError(ctx, err), nil
			}
			cookie.Secure = access.ClientFrom(ctx).Secure
			s.app.Log.Info("signed in with single sign-on", "user", u.Username)
			if next == "" {
				next = "/"
			}
			// the state cookie is spent; the session cookie takes over
			return &ssoRedirect{Status: http.StatusSeeOther, Location: s.app.Cfg.URLBase + next, SetCookie: *cookie}, nil
		})

	stags := []string{"Settings"}
	huma.Register(s.api, huma.Operation{OperationID: "settings-get-sso", Method: http.MethodGet, Path: "/api/v1/settings/sso", Tags: stags},
		func(ctx context.Context, _ *struct{}) (*struct{ Body SSOSettings }, error) {
			cfg, err := s.app.Settings.SSO(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			if cfg.ClientSecret != "" {
				cfg.ClientSecret = secretMask
			}
			return &struct{ Body SSOSettings }{SSOSettings{SSO: cfg, RedirectURL: s.redirectURL(ctx)}}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "settings-put-sso", Method: http.MethodPut, Path: "/api/v1/settings/sso", Tags: stags,
		Summary: "Save single sign-on settings (send the masked secret to keep the stored one)"},
		func(ctx context.Context, in *struct{ Body settings.SSO }) (*struct{ Body SSOSettings }, error) {
			cur, err := s.app.Settings.SSO(ctx)
			if err != nil {
				return nil, toHTTPError(err)
			}
			cfg := in.Body
			if cfg.ClientSecret == secretMask || cfg.ClientSecret == "" {
				cfg.ClientSecret = cur.ClientSecret
			}
			cfg.Issuer = strings.TrimSpace(cfg.Issuer)
			if cfg.Enabled {
				if cfg.Issuer == "" || cfg.ClientID == "" {
					return nil, huma.Error400BadRequest("the issuer and client id are needed")
				}
				if _, err := s.app.SSO.Provider(ctx, cfg.Issuer); err != nil {
					return nil, huma.Error400BadRequest(err.Error())
				}
			}
			if err := s.app.Settings.Set(ctx, settings.KeySSO, cfg); err != nil {
				return nil, toHTTPError(err)
			}
			s.app.Bus.Changed("settings", "updated", 0)
			if cfg.ClientSecret != "" {
				cfg.ClientSecret = secretMask
			}
			return &struct{ Body SSOSettings }{SSOSettings{SSO: cfg, RedirectURL: s.redirectURL(ctx)}}, nil
		})
}
