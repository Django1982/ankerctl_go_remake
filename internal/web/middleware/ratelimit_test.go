package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimit_BlocksAfterLimit(t *testing.T) {
	h := RateLimit(2, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodGet, "/api/version", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want %d", i+1, w.Code, http.StatusOK)
		}
	}

	r := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	r.RemoteAddr = "127.0.0.1:12345"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header")
	}
}

func TestRateLimit_StaticAndHealthAreExcluded(t *testing.T) {
	h := RateLimit(1, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	paths := []string{"/static/app.js", "/api/health", "/static/app.js", "/api/health"}
	for _, path := range paths {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("path %s status = %d, want %d", path, w.Code, http.StatusOK)
		}
	}
}

func TestRateLimit_WebsocketPathsAreExcluded(t *testing.T) {
	h := RateLimit(1, time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// WebSocket paths must never be rate-limited: rapid reconnects during
	// PPPP restart cycles would otherwise cause 429s and keep badges yellow.
	paths := []string{"/ws/mqtt", "/ws/ctrl", "/ws/pppp-state", "/ws/upload", "/ws/mqtt", "/ws/ctrl"}
	for _, path := range paths {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("path %s status = %d, want %d (WebSocket paths must bypass rate limit)", path, w.Code, http.StatusOK)
		}
	}
}

