package apitiming_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/apitiming"
)

// TestMiddleware: the response says how long it took and where the time went.
func TestMiddleware(t *testing.T) {
	h := apitiming.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done := apitiming.Span(r.Context(), "source")
		time.Sleep(2 * time.Millisecond)
		done()
		apitiming.Span(r.Context(), "source")() // the same phase adds up
		_, _ = w.Write([]byte("ok"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))

	got := rec.Header().Get("Server-Timing")
	if !strings.HasPrefix(got, "total;dur=") || !strings.Contains(got, "source;dur=") {
		t.Fatalf("Server-Timing = %q", got)
	}
	if strings.Count(got, "source;dur=") != 1 {
		t.Fatalf("a phase reported twice: %q", got)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body %q", rec.Body.String())
	}
}

// TestHandlerWithoutTimings: Span outside a timed request is a no-op.
func TestHandlerWithoutTimings(t *testing.T) {
	rec := httptest.NewRecorder()
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apitiming.Span(r.Context(), "db")()
		w.WriteHeader(http.StatusNoContent)
	})
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != http.StatusNoContent || rec.Header().Get("Server-Timing") != "" {
		t.Fatalf("code %d, header %q", rec.Code, rec.Header().Get("Server-Timing"))
	}
}
