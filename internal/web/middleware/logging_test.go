package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// recordHandler captures slog records for test assertions.
type recordHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *recordHandler) WithGroup(_ string) slog.Handler       { return h }

func (h *recordHandler) last() slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.records[len(h.records)-1]
}

func (h *recordHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

// attrValue returns the string value of the first attribute matching the key.
func attrValue(rec slog.Record, key string) string {
	var val string
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			val = a.Value.String()
			return false
		}
		return true
	})
	return val
}

func TestAccessLogger(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		status    int
		wantLevel slog.Level
	}{
		{
			name:      "static file logged at DEBUG",
			path:      "/static/foo.js",
			status:    200,
			wantLevel: slog.LevelDebug,
		},
		{
			name:      "static file 500 still logged at DEBUG",
			path:      "/static/broken.js",
			status:    500,
			wantLevel: slog.LevelDebug,
		},
		{
			name:      "2xx response logged at INFO",
			path:      "/api/health",
			status:    200,
			wantLevel: slog.LevelInfo,
		},
		{
			name:      "201 response logged at INFO",
			path:      "/api/upload",
			status:    201,
			wantLevel: slog.LevelInfo,
		},
		{
			name:      "301 redirect logged at INFO",
			path:      "/api/redirect",
			status:    301,
			wantLevel: slog.LevelInfo,
		},
		{
			name:      "400 bad request logged at WARN",
			path:      "/api/bad",
			status:    400,
			wantLevel: slog.LevelWarn,
		},
		{
			name:      "401 unauthorized logged at WARN",
			path:      "/api/secret",
			status:    401,
			wantLevel: slog.LevelWarn,
		},
		{
			name:      "404 not found logged at WARN",
			path:      "/api/missing",
			status:    404,
			wantLevel: slog.LevelWarn,
		},
		{
			name:      "500 internal error logged at ERROR",
			path:      "/api/crash",
			status:    500,
			wantLevel: slog.LevelError,
		},
		{
			name:      "502 bad gateway logged at ERROR",
			path:      "/api/gateway",
			status:    502,
			wantLevel: slog.LevelError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &recordHandler{}
			logger := slog.New(rec)

			handler := AccessLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))

			r := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)

			if rec.count() != 1 {
				t.Fatalf("expected 1 log record, got %d", rec.count())
			}

			got := rec.last()
			if got.Level != tt.wantLevel {
				t.Errorf("level = %v, want %v", got.Level, tt.wantLevel)
			}
			if got.Message != "HTTP request" {
				t.Errorf("message = %q, want %q", got.Message, "HTTP request")
			}
		})
	}
}

func TestAccessLogger_RequestIDInLog(t *testing.T) {
	rec := &recordHandler{}
	logger := slog.New(rec)

	// chimw.RequestID sets a request ID in context.
	chain := chimw.RequestID(AccessLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	chain.ServeHTTP(w, r)

	if rec.count() != 1 {
		t.Fatalf("expected 1 log record, got %d", rec.count())
	}

	rid := attrValue(rec.last(), "request_id")
	if rid == "" {
		t.Error("request_id attribute is empty; expected non-empty value from RequestID middleware")
	}
}

func TestAccessLogger_LogAttributes(t *testing.T) {
	rec := &recordHandler{}
	logger := slog.New(rec)

	handler := AccessLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodPost, "/api/upload", nil)
	r.RemoteAddr = "10.0.0.1:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if rec.count() != 1 {
		t.Fatalf("expected 1 log record, got %d", rec.count())
	}

	entry := rec.last()

	wantAttrs := map[string]string{
		"method": "POST",
		"path":   "/api/upload",
		"status": "200",
		"ip":     "10.0.0.1",
	}

	for key, want := range wantAttrs {
		got := attrValue(entry, key)
		if got != want {
			t.Errorf("attr %q = %q, want %q", key, got, want)
		}
	}

	dur := attrValue(entry, "duration")
	if dur == "" {
		t.Error("duration attribute is missing")
	}
}

func TestAccessLogger_NilLogger(t *testing.T) {
	// Passing nil should not panic — falls back to slog.Default().
	handler := AccessLogger(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAccessLogger_DefaultStatusZeroIs200(t *testing.T) {
	// When a handler doesn't call WriteHeader, the implicit status is 200.
	// chi's WrapResponseWriter returns 0 for Status() if WriteHeader was never
	// called. The access logger should still log at INFO (not WARN/ERROR).
	rec := &recordHandler{}
	logger := slog.New(rec)

	handler := AccessLogger(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately not calling WriteHeader — implicit 200.
		_, _ = w.Write([]byte("ok"))
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if rec.count() != 1 {
		t.Fatalf("expected 1 log record, got %d", rec.count())
	}
	got := rec.last()
	// Status 0 or 200 — either way should NOT be ERROR or WARN.
	if got.Level >= slog.LevelWarn {
		t.Errorf("implicit 200 response logged at %v, expected INFO or DEBUG", got.Level)
	}
}
