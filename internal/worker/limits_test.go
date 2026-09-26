package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLimitsFollowTheServer: the limits set in System → Workers arrive with
// every lease, so a running worker picks up a change without a restart, and
// its own settings only fill in what the server leaves at 0.
func TestLimitsFollowTheServer(t *testing.T) {
	answer := map[string]any{"concurrent": 3, "pageConcurrency": 7}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(rw).Encode(answer)
	}))
	defer srv.Close()
	w := &Worker{cfg: Config{ServerURL: srv.URL, Key: "test", HTTP: srv.Client(), PageConcurrency: 5}}
	if w.taskLimit() != 1 || w.pageLimit() != 5 {
		t.Fatalf("before any answer: tasks=%d pages=%d, want 1 and 5", w.taskLimit(), w.pageLimit())
	}
	if _, err := w.lease(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.taskLimit() != 3 || w.pageLimit() != 7 {
		t.Fatalf("after a lease: tasks=%d pages=%d, want 3 and 7", w.taskLimit(), w.pageLimit())
	}
	// 0 for pages hands it back to the worker's own setting
	answer = map[string]any{"concurrent": 2, "pageConcurrency": 0}
	if _, err := w.lease(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.taskLimit() != 2 || w.pageLimit() != 5 {
		t.Fatalf("after a change: tasks=%d pages=%d, want 2 and 5", w.taskLimit(), w.pageLimit())
	}
	// an older server says nothing: the last limits stay
	answer = map[string]any{}
	if _, err := w.lease(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.taskLimit() != 2 {
		t.Fatalf("an older server's answer changed the limit: %d", w.taskLimit())
	}
	// the worker's own task limit wins over the server's
	w.cfg.Concurrent = 1
	if w.taskLimit() != 1 {
		t.Fatalf("MANGARR_WORKER_CONCURRENT ignored: %d", w.taskLimit())
	}
}
