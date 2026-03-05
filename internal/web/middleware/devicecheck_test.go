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

