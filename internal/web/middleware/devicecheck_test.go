package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type deviceStateStub struct {
	login       bool
	unsupported bool
}

func (s *deviceStateStub) IsLoggedIn() bool         { return s.login }
func (s *deviceStateStub) IsUnsupportedDevice() bool { return s.unsupported }

func TestRequirePrinter_PrinterPathWithoutLogin_Returns503(t *testing.T) {
	state := &deviceStateStub{login: false}
	h := RequirePrinter(state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestRequirePrinter_NonPrinterPath_Allows(t *testing.T) {
	state := &deviceStateStub{login: false}
	h := RequirePrinter(state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestBlockUnsupportedDevice_PrinterPathWhenUnsupported_Returns503(t *testing.T) {
	state := &deviceStateStub{login: true, unsupported: true}
	h := BlockUnsupportedDevice(state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodPost, "/api/files/local", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestBlockUnsupportedDevice_WebSocketPathsWhenUnsupported_Returns503(t *testing.T) {
	wsPaths := []string{
		"/ws/ctrl",
		"/ws/video",
		"/ws/mqtt",
		"/ws/pppp-state",
		"/ws/upload",
	}

	for _, path := range wsPaths {
		t.Run(path, func(t *testing.T) {
			state := &deviceStateStub{login: true, unsupported: true}
			h := BlockUnsupportedDevice(state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			r := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("path %s: status = %d, want %d", path, w.Code, http.StatusServiceUnavailable)
			}
		})
	}
}

func TestBlockUnsupportedDevice_WebSocketPathsWhenSupported_Allows(t *testing.T) {
	wsPaths := []string{
		"/ws/ctrl",
		"/ws/video",
		"/ws/mqtt",
		"/ws/pppp-state",
		"/ws/upload",
	}

	for _, path := range wsPaths {
		t.Run(path, func(t *testing.T) {
			state := &deviceStateStub{login: true, unsupported: false}
			h := BlockUnsupportedDevice(state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			r := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("path %s: status = %d, want %d", path, w.Code, http.StatusOK)
			}
		})
	}
}

func TestRequirePrinter_WebSocketPathWithoutLogin_Returns503(t *testing.T) {
	state := &deviceStateStub{login: false}
	h := RequirePrinter(state)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/ws/mqtt", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

