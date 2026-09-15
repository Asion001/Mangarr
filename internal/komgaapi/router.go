package komgaapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// router builds the API. Routes sit at the root, like Komga's.
func (s *Service) router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, cors, middleware.StripSlashes, s.logRequests, middleware.Recoverer)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) { writeError(w, r, http.StatusNotFound, "Not Found") })
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusMethodNotAllowed, "Method Not Allowed")
	})

	// public: KMReader checks that this is a Komga server before logging in
	r.Get("/api/v1/client-settings/global/list", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]any{}) })

	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		s.routes(r)
	})
	return r
}

// routes registers every protected route (see testdata/routes.txt).
func (s *Service) routes(r chi.Router) {
	a := &authHandlers{s}
	r.Get("/api/v2/users/me", a.me)
	r.Get("/api/v2/users/me/api-keys", a.listKeys)
	r.Post("/api/v2/users/me/api-keys", a.createKey)
	r.Delete("/api/v2/users/me/api-keys/{id}", a.deleteKey)
	r.Post("/api/logout", a.logout)
	r.Get("/api/v1/client-settings/user/list", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]any{}) })

	l := &libraryHandlers{s}
	r.Get("/api/v1/libraries", l.list)
	r.Get("/api/v1/libraries/{id}", l.get)
}

// cors lets browser-based clients call the API (credentials go in headers).
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-API-Key, X-Auth-Token")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		h.Set("Access-Control-Expose-Headers", "X-Auth-Token, Content-Disposition")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logRequests logs requests at debug level (errors at warn).
func (s *Service) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		st := ww.Status()
		args := []any{"method", r.Method, "path", r.URL.Path, "status", st, "duration", time.Since(start).Round(time.Millisecond),
			"client", ClientName(r.UserAgent())}
		switch {
		case st >= 500:
			s.deps.Log.Warn("Komga API request failed", args...)
		case st == http.StatusNotFound && !strings.Contains(r.URL.Path, "/thumbnail"):
			s.deps.Log.Info("Komga API route not supported", args...) // shows what apps still miss
		default:
			s.deps.Log.Debug("Komga API request", args...)
		}
	})
}
