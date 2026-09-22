package worker

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestOutputIsChunkedBelowProxyLimits(t *testing.T) {
	const chunkBytes = 1 << 20
	data := bytes.Repeat([]byte("x"), chunkBytes+17)
	var got bytes.Buffer
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.Header.Get("X-Mangarr-Chunk") != strconv.Itoa(requests) || r.Header.Get("X-Mangarr-Chunks") != "2" {
			t.Errorf("request %d: %s %s/%s", requests, r.Method, r.Header.Get("X-Mangarr-Chunk"), r.Header.Get("X-Mangarr-Chunks"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if len(body) > chunkBytes {
			t.Errorf("request body = %d bytes", len(body))
		}
		got.Write(body)
		rw.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	w := &Worker{cfg: Config{ServerURL: srv.URL, Key: "test", HTTP: srv.Client()}, welcome: Welcome{OutputChunkBytes: chunkBytes}}
	if err := w.output(context.Background(), 42, data); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || !bytes.Equal(got.Bytes(), data) {
		t.Fatalf("requests=%d bytes=%d, want 2 and %d", requests, got.Len(), len(data))
	}
}

func TestOutputFallsBackToOneRequestForOldServer(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("X-Mangarr-Chunk") != "1" || r.Header.Get("X-Mangarr-Chunks") != "1" {
			t.Errorf("chunk headers: %s/%s", r.Header.Get("X-Mangarr-Chunk"), r.Header.Get("X-Mangarr-Chunks"))
		}
		rw.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	w := &Worker{cfg: Config{ServerURL: srv.URL, Key: "test", HTTP: srv.Client()}}
	if err := w.output(context.Background(), 42, []byte("compatible")); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d, want 1", requests)
	}
}
