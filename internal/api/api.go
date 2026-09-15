// Package api exposes the REST API (OpenAPI via huma), the SSE event stream
// and the embedded web UI.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Asion001/mangarr/internal/app"
	"github.com/Asion001/mangarr/internal/auth"
	"github.com/Asion001/mangarr/internal/version"
)

type Server struct {
	app *app.App
	api huma.API
}

func init() {
	// Every list the API returns is a JSON array (never null), so don't mark
	// arrays nullable in the schema (keeps generated client types simple).
	huma.DefaultArrayNullable = false
}

// New returns the root HTTP handler.
func New(a *app.App) http.Handler {
	r := chi.NewMux()
	r.Use(middleware.RealIP, middleware.Recoverer, requestLogger(a.Log))

	base := a.Cfg.URLBase
	sub := chi.NewMux()
	s := &Server{app: a}
	sub.Use(s.authMiddleware, s.maintenanceMiddleware)

	cfg := huma.DefaultConfig("mangarr", version.Version)
	cfg.Info.Description = "Sonarr-style PVR for manga. All endpoints accept an X-Api-Key header."
	cfg.OpenAPIPath = "/api/openapi"
	cfg.DocsPath = "/api/docs"
	cfg.SchemasPath = "/api/schemas"
	cfg.CreateHooks = nil // no $schema links in responses
	if base != "" {
		cfg.Servers = []*huma.Server{{URL: base}}
	}
	cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"apiKey": {Type: "apiKey", In: "header", Name: "X-Api-Key"},
	}
	cfg.Security = []map[string][]string{{"apiKey": {}}}
	cfg.Components.Schemas = huma.NewMapRegistry("#/components/schemas/", schemaNamer)
	s.api = humachi.New(sub, cfg)

	s.registerAuth()
	s.registerSystem()
	s.registerSettings()
	s.registerModules()
	for _, reg := range extraRoutes {
		reg(s)
	}

	sub.Get("/api/v1/events", s.handleEvents)
	sub.Get("/ping", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("pong")) })
	sub.Handle("/*", s.staticHandler())

	if base != "" {
		r.Mount(base, http.StripPrefix(base, sub))
		r.Get("/", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, base+"/", http.StatusFound) })
	} else {
		r.Mount("/", sub)
	}
	return r
}

// schemaNamer prefixes types from module/interface packages so equally named
// Go types (model.Chapter vs source.Chapter) get distinct schema names.
func schemaNamer(t reflect.Type, hint string) string {
	name := huma.DefaultSchemaNamer(t, hint)
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Array || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	pkg := t.PkgPath()
	switch {
	case strings.HasSuffix(pkg, "/internal/modules/source"):
		return "Source" + name
	case strings.HasSuffix(pkg, "/internal/modules/metadata"):
		return "Metadata" + name
	case strings.HasSuffix(pkg, "/internal/modules/upscale"):
		return "Upscale" + name
	case strings.HasSuffix(pkg, "/internal/modules/library"):
		return "Library" + name
	case strings.HasSuffix(pkg, "/internal/modules"):
		return "Module" + name
	case strings.HasSuffix(pkg, "/internal/decision"):
		return "Decision" + name
	case strings.HasSuffix(pkg, "/internal/cleanup"):
		return "Cleanup" + name
	case strings.HasSuffix(pkg, "/internal/health"):
		return "Health" + name
	case strings.HasSuffix(pkg, "/internal/backup"):
		return "Backup" + name
	case strings.HasSuffix(pkg, "/internal/metadataagg"):
		return "Metadata" + name
	}
	return name
}

// extraRoutes lets feature files register their operations.
var extraRoutes []func(*Server)

func register(fn func(*Server)) { extraRoutes = append(extraRoutes, fn) }

var publicPaths = map[string]bool{
	"/api/v1/auth/status": true,
	"/api/v1/auth/login":  true,
	"/api/v1/auth/setup":  true,
	"/ping":               true,
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// UI assets and public endpoints don't need auth; the UI handles login.
		if publicPaths[p] || !strings.HasPrefix(p, "/api/") {
			if principal := s.app.Auth.Authenticate(r); principal != "" {
				r = r.WithContext(auth.WithPrincipal(r.Context(), principal))
			}
			next.ServeHTTP(w, r)
			return
		}
		principal := s.app.Auth.Authenticate(r)
		if principal == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401,"detail":"login or X-Api-Key required"}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), principal)))
	})
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/events" {
				log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "duration", time.Since(start))
			}
		})
	}
}

// ---- helpers ---------------------------------------------------------------

type Empty struct{}

type IDPath struct {
	ID int64 `path:"id"`
}

// toHTTPError maps common errors to huma errors.
func toHTTPError(err error) error {
	if err == nil {
		return nil
	}
	var se huma.StatusError
	if errors.As(err, &se) {
		return err
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return huma.Error504GatewayTimeout(err.Error())
	}
	var ve validationError
	if errors.As(err, &ve) {
		return huma.Error400BadRequest(ve.Error())
	}
	return huma.Error500InternalServerError(err.Error())
}

var ErrNotFound = errors.New("not found")

// splitList flattens a repeated query parameter whose values may also be
// comma-separated ("a,b" and "a&x=b" both work).
func splitList(values []string) []string {
	var out []string
	for _, v := range values {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

type validationError struct{ msg string }

func (v validationError) Error() string { return v.msg }

func badRequest(msg string) error { return validationError{msg} }
