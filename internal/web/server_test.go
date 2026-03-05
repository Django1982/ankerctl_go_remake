package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	mw "github.com/django1982/ankerctl/internal/web/middleware"
	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
)

func TestRegisterRoutes_HealthEndpoint(t *testing.T) {
	s := NewServer(nil)
	s.router = chi.NewRouter()
	s.registerRoutes()

	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Fatalf("body = %q, want %q", got, "{\"status\":\"ok\"}\\n")
	}
}

func TestRegisterRoutes_VersionEndpoint(t *testing.T) {
	s := NewServer(nil)
	s.router = chi.NewRouter()
	s.registerRoutes()

	r := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMiddlewareOrder_DeviceChecksBeforeAuth(t *testing.T) {
	s := NewServer(nil, WithAPIKey("test-api-key-1234"))
	s.login = false
	s.sessionManager = mw.NewSessionManager([]byte("secret"))
	s.router = chi.NewRouter()
	s.router.Use(chimw.Recoverer)
	s.router.Use(chimw.RequestID)
	s.router.Use(mw.SecurityHeaders)
	s.router.Use(mw.RequirePrinter(s))
	s.router.Use(mw.BlockUnsupportedDevice(s))
	s.router.Use(mw.Auth(s))
	s.registerRoutes()

	r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, r)

	// Must be 503 from RequirePrinter, not 401 from auth middleware.
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}
