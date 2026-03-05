package middleware

import (
	"net/http/httptest"
	"testing"
)

func TestSessionManager_RoundtripAuthenticated(t *testing.T) {
	sm := NewSessionManager([]byte("top-secret-key"))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	sm.SetAuthenticated(w, r, true)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookie to be set")
	}

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(cookies[0])

	if !sm.IsAuthenticated(r2) {
		t.Fatal("expected authenticated request")
	}
}

func TestSessionManager_TamperedCookieRejected(t *testing.T) {
	sm := NewSessionManager([]byte("top-secret-key"))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	sm.SetAuthenticated(w, r, true)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected cookie to be set")
	}

	tampered := *cookies[0]
	tampered.Value = tampered.Value + "x"

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.AddCookie(&tampered)

	if sm.IsAuthenticated(r2) {
		t.Fatal("tampered cookie should not authenticate")
	}
}

func TestSessionManager_ClearCookie(t *testing.T) {
	sm := NewSessionManager([]byte("top-secret-key"))

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	sm.SetAuthenticated(w, r, false)

	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected clear cookie to be set")
	}
	if cookies[0].MaxAge != -1 {
		t.Fatalf("MaxAge = %d, want -1", cookies[0].MaxAge)
	}
}
