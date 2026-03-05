package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type authTestState struct {
	apiKey string
	login  bool
	sm     *SessionManager
}

func (s *authTestState) APIKey() string                 { return s.apiKey }
func (s *authTestState) IsLoggedIn() bool               { return s.login }
func (s *authTestState) SessionManager() *SessionManager { return s.sm }

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuth_NoAPIKey_AllowsPost(t *testing.T) {
	state := &authTestState{apiKey: "", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_PostWithoutAuth_ReturnsUnauthorized(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuth_PostWithHeader_Allows(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
	r.Header.Set("X-Api-Key", "test-api-key-1234")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_GetOpenPath_WithoutAuth_Allows(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_GetProtectedPath_WithoutAuth_Unauthorized(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodGet, "/api/history", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuth_DebugPrefix_RequiresAuth(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodGet, "/api/debug/state", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuth_SetupPathWithoutLogin_Allows(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: false, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodPost, "/api/ankerctl/config/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_SetupPathWithLogin_RequiresAuth(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodPost, "/api/ankerctl/config/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuth_ApiKeyQuery_SetsCookieAndRedirects(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodGet, "/api/history?apikey=test-api-key-1234&foo=1", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	if loc := w.Header().Get("Location"); loc != "/api/history?foo=1" {
		t.Fatalf("location = %q, want %q", loc, "/api/history?foo=1")
	}
	if len(w.Result().Cookies()) == 0 {
		t.Fatal("expected session cookie to be set")
	}
}

func TestAuth_SessionCookie_AllowsProtectedPost(t *testing.T) {
	sm := NewSessionManager([]byte("secret"))
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: sm}
	h := Auth(state)(okHandler())

	cookieWriter := httptest.NewRecorder()
	baseReq := httptest.NewRequest(http.MethodGet, "/", nil)
	sm.SetAuthenticated(cookieWriter, baseReq, true)
	cookies := cookieWriter.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}

	r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
	r.AddCookie(cookies[0])
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_StaticPath_AlwaysAllowed(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodGet, "/static/app.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
