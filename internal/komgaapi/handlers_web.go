package komgaapi

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Mihon's Komga extension removes /api/v1 for manga URLs and replaces
// /api/v1/books with /book for chapter URLs. The web UI handles login.
func (s *Service) webRoutes(r chi.Router) {
	r.Get("/series/{id}", s.webRedirect("/series/"))
	r.Get("/book/{id}", s.webRedirect("/read/"))
	r.Get("/books/{id}", s.webRedirect("/read/"))
	r.Get("/readlists/{id}", s.webRedirect("/"))
}

func (s *Service) webRedirect(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		base := ""
		if s.deps.Settings != nil {
			if g, err := s.deps.Settings.General(r.Context()); err == nil {
				base = strings.TrimRight(g.PublicURL, "/")
			}
		}
		if base == "" {
			port := "8787"
			if _, p, err := net.SplitHostPort(s.deps.WebListen); err == nil {
				port = p
			}
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			host := (&url.URL{Host: r.Host}).Hostname()
			base = scheme + "://" + net.JoinHostPort(host, port) + strings.TrimRight(s.deps.URLBase, "/")
		}
		target := base + path
		if path != "/" {
			// Komga IDs are mangarr IDs; unknown IDs are left to the web UI.
			target += url.PathEscape(chi.URLParam(r, "id"))
		}
		http.Redirect(w, r, target, http.StatusFound)
	}
}
