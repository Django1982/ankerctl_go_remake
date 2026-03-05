package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/django1982/ankerctl/internal/config"
	"github.com/django1982/ankerctl/internal/db"
	"github.com/go-chi/chi/v5"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfgDir := t.TempDir()
	cfgMgr, err := config.NewManager(cfgDir)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return New(cfgMgr, database, nil, nil, false)
}

func TestGeneralEndpoints(t *testing.T) {
	h := newTestHandler(t)

	w := httptest.NewRecorder()
	h.Health(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("health status=%d", w.Code)
	}
	if got := w.Body.String(); got != "{\"status\":\"ok\"}\n" {
		t.Fatalf("health body=%q", got)
	}

	w = httptest.NewRecorder()
	h.Version(w, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("version status=%d", w.Code)
	}
	var payload map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	// OctoPrint-compatible shape (matches Python __init__.py)
	if payload["api"] != "0.1" || payload["server"] != "1.9.0" || payload["text"] != "OctoPrint 1.9.0" {
		t.Fatalf("unexpected version payload: %#v", payload)
	}
}

func TestHistoryShape(t *testing.T) {
	h := newTestHandler(t)
	_, err := h.db.RecordStart("part.gcode", "task-1")
	if err != nil {
		t.Fatalf("RecordStart: %v", err)
	}
	w := httptest.NewRecorder()
	h.HistoryList(w, httptest.NewRequest(http.MethodGet, "/api/history", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d", w.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Python-compatible shape: {"entries": [...], "total": N}
	if _, ok := payload["entries"]; !ok {
		t.Fatalf("missing 'entries' key: %#v", payload)
	}
	if _, ok := payload["total"]; !ok {
		t.Fatalf("missing 'total' key: %#v", payload)
	}
}

func TestTimelapseTraversalRejected(t *testing.T) {
	h := newTestHandler(t)
	r := httptest.NewRequest(http.MethodGet, "/api/timelapse/ignored", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("filename", "../etc/passwd")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()
	h.TimelapseDownload(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d", w.Code, http.StatusBadRequest)
	}
}

func TestDebugLogsTraversalRejected(t *testing.T) {
	h := newTestHandler(t)
	h.devMode = true
	logDir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("ANKERCTL_LOG_DIR", logDir)

	r := httptest.NewRequest(http.MethodGet, "/api/debug/logs/ignored", nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("filename", "../../secret.log")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
	w := httptest.NewRecorder()
	h.DebugLogsContent(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=%d", w.Code, http.StatusBadRequest)
	}
}
