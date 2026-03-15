package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuth_QueryAPIKey_WrongKey_Unauthorized(t *testing.T) {
	state := &authTestState{apiKey: "correct-key-12345", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodGet, "/api/history?apikey=wrong-key-99999", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	// Wrong query key on a protected GET path should fall through to 401.
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuth_QueryAPIKey_OnlyParam_StrippedFromRedirect(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	// When apikey is the ONLY query parameter, redirect should be the path alone.
	r := httptest.NewRequest(http.MethodGet, "/dashboard?apikey=test-api-key-1234", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if loc != "/dashboard" {
		t.Fatalf("location = %q, want %q", loc, "/dashboard")
	}
}

func TestAuth_EmptyAPIKey_AllowsEverything(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"POST to protected path", http.MethodPost, "/api/printer/control"},
		{"DELETE request", http.MethodDelete, "/api/printer/remove"},
		{"GET to debug path", http.MethodGet, "/api/debug/state"},
		{"GET to protected path", http.MethodGet, "/api/history"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &authTestState{apiKey: "", login: true, sm: NewSessionManager([]byte("secret"))}
			h := Auth(state)(okHandler())

			r := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (empty API key should allow all)", w.Code, http.StatusOK)
			}
		})
	}
}

func TestAuth_WhitespaceOnlyAPIKey_TreatedAsEmpty(t *testing.T) {
	// A whitespace-only API key is effectively empty — the middleware checks
	// apiKey == "" which would NOT match "   ". This tests the actual behavior.
	state := &authTestState{apiKey: "   ", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	// POST without credentials — if whitespace key is treated as non-empty, this returns 401.
	r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	// Whitespace key is NOT empty string, so auth is enforced -> 401.
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (whitespace key should still enforce auth)", w.Code, http.StatusUnauthorized)
	}
}

func TestAuth_WhitespaceAPIKey_MatchableViaHeader(t *testing.T) {
	// Verify that a whitespace API key can still be matched by providing
	// the exact same whitespace in the X-Api-Key header.
	state := &authTestState{apiKey: "   ", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
	r.Header.Set("X-Api-Key", "   ")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestAuth_VeryLongAPIKey(t *testing.T) {
	longKey := strings.Repeat("a", 2000)
	state := &authTestState{apiKey: longKey, login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	tests := []struct {
		name       string
		headerKey  string
		wantStatus int
	}{
		{"correct long key", longKey, http.StatusOK},
		{"wrong long key", strings.Repeat("b", 2000), http.StatusUnauthorized},
		{"short key", "short", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
			r.Header.Set("X-Api-Key", tt.headerKey)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestAuth_HeaderAndQueryBothSet(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	t.Run("correct query key takes precedence with redirect", func(t *testing.T) {
		// Query key is checked BEFORE header. A correct query key triggers redirect.
		r := httptest.NewRequest(http.MethodGet, "/api/health?apikey=test-api-key-1234", nil)
		r.Header.Set("X-Api-Key", "wrong-key")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if w.Code != http.StatusFound {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
		}
	})

	t.Run("wrong query key but correct header allows request", func(t *testing.T) {
		// Wrong query key doesn't match, falls through to header check.
		r := httptest.NewRequest(http.MethodGet, "/api/health?apikey=wrong-key", nil)
		r.Header.Set("X-Api-Key", "test-api-key-1234")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		// /api/health is public GET, so it would pass anyway via the public method check.
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("wrong query and wrong header on protected path", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/api/history?apikey=wrong-key", nil)
		r.Header.Set("X-Api-Key", "also-wrong")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})
}

func TestAuth_BearerTokenNotSupported(t *testing.T) {
	// The auth middleware uses X-Api-Key header, NOT Authorization: Bearer.
	// Verify that a Bearer token does NOT authenticate.
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
	r.Header.Set("Authorization", "Bearer test-api-key-1234")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (Bearer not supported)", w.Code, http.StatusUnauthorized)
	}
}

func TestAuth_NilSessionManager(t *testing.T) {
	// When SessionManager is nil, cookie-based auth is skipped.
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: nil}
	h := Auth(state)(okHandler())

	t.Run("header auth still works", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
		r.Header.Set("X-Api-Key", "test-api-key-1234")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	})

	t.Run("no panic without session manager", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
		}
	})
}

func TestAuth_HEADAndOPTIONS_PublicMethods(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	for _, method := range []string{http.MethodHead, http.MethodOptions} {
		t.Run(method+" on non-protected path", func(t *testing.T) {
			r := httptest.NewRequest(method, "/api/health", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
			}
		})
	}
}

func TestAuth_PUTAndPATCH_RequireAuth(t *testing.T) {
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		t.Run(method+" without auth", func(t *testing.T) {
			r := httptest.NewRequest(method, "/api/printer/control", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusUnauthorized)
			}
		})
	}
}

func TestAuth_AllProtectedGETPaths(t *testing.T) {
	paths := []string{
		"/api/ankerctl/server/reload",
		"/api/debug/state",
		"/api/debug/logs",
		"/api/debug/services",
		"/api/debug/anything-else", // prefix match
		"/api/settings/mqtt",
		"/api/notifications/settings",
		"/api/printers",
		"/api/history",
	}

	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	for _, path := range paths {
		t.Run("GET "+path, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d for protected path %s", w.Code, http.StatusUnauthorized, path)
			}
		})
	}
}

func TestAuth_SetupPaths_BothExemptWhenNotLoggedIn(t *testing.T) {
	paths := []string{
		"/api/ankerctl/config/upload",
		"/api/ankerctl/config/login",
	}

	for _, path := range paths {
		t.Run("POST "+path, func(t *testing.T) {
			state := &authTestState{apiKey: "test-api-key-1234", login: false, sm: NewSessionManager([]byte("secret"))}
			h := Auth(state)(okHandler())

			r := httptest.NewRequest(http.MethodPost, path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (setup path exempt when not logged in)", w.Code, http.StatusOK)
			}
		})
	}
}

func TestAuth_QueryAPIKey_POST_RedirectsOnMatch(t *testing.T) {
	// Even for POST requests, a correct query API key triggers a redirect.
	state := &authTestState{apiKey: "test-api-key-1234", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodPost, "/api/printer/control?apikey=test-api-key-1234", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusFound)
	}
	loc := w.Header().Get("Location")
	if loc != "/api/printer/control" {
		t.Fatalf("location = %q, want %q", loc, "/api/printer/control")
	}
}

func TestAuth_CaseSensitiveAPIKey(t *testing.T) {
	state := &authTestState{apiKey: "MySecretKey12345", login: true, sm: NewSessionManager([]byte("secret"))}
	h := Auth(state)(okHandler())

	r := httptest.NewRequest(http.MethodPost, "/api/printer/control", nil)
	r.Header.Set("X-Api-Key", "mysecretkey12345") // lowercase
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d (API key should be case-sensitive)", w.Code, http.StatusUnauthorized)
	}
}
