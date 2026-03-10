package middleware

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
)

var protectedGETPaths = map[string]bool{
	"/api/ankerctl/server/reload": true,
	"/api/debug/state":            true,
	"/api/debug/logs":             true,
	"/api/debug/services":         true,
	"/api/settings/mqtt":          true,
	"/api/notifications/settings": true,
	"/api/printers":               true,
	"/api/history":                true,
}

var setupPaths = map[string]bool{
	"/api/ankerctl/config/upload": true,
	"/api/ankerctl/config/login":  true,
}

// AuthState provides server runtime state required by auth middleware.
type AuthState interface {
	APIKey() string
	IsLoggedIn() bool
	SessionManager() *SessionManager
}

// Auth enforces API key and session-based auth with Python-compatible rules.
func Auth(state AuthState) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := state.APIKey()
			if apiKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			if strings.HasPrefix(r.URL.Path, "/static/") {
				next.ServeHTTP(w, r)
				return
			}

			if keyFromQuery := r.URL.Query().Get("apikey"); keyFromQuery != "" {
				if secureEquals(keyFromQuery, apiKey) {
					sm := state.SessionManager()
					if sm != nil {
						sm.SetAuthenticated(w, r, true)
					}
					http.Redirect(w, r, stripAPIKeyParam(r.URL), http.StatusFound)
					return
				}
			}

			if secureEquals(r.Header.Get("X-Api-Key"), apiKey) {
				next.ServeHTTP(w, r)
				return
			}

			if isPublicMethod(r.Method) && !isProtectedGETPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			if setupPaths[r.URL.Path] && !state.IsLoggedIn() {
				next.ServeHTTP(w, r)
				return
			}

			sm := state.SessionManager()
			if sm != nil && sm.IsAuthenticated(r) {
				next.ServeHTTP(w, r)
				return
			}

			writeJSONError(w, http.StatusUnauthorized, "Unauthorized. Provide API key via X-Api-Key header or ?apikey=...")
		})
	}
}

func isPublicMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func isProtectedGETPath(path string) bool {
	if strings.HasPrefix(path, "/api/debug/") {
		return true
	}
	return protectedGETPaths[path]
}

func stripAPIKeyParam(u *url.URL) string {
	copyURL := *u
	q := copyURL.Query()
	q.Del("apikey")
	copyURL.RawQuery = q.Encode()
	if copyURL.RawQuery == "" {
		return copyURL.Path
	}
	return copyURL.Path + "?" + copyURL.RawQuery
}

func secureEquals(a, b string) bool {
	// Do NOT short-circuit on length difference — that leaks the key length
	// via timing. subtle.ConstantTimeCompare already returns 0 when lengths
	// differ, in constant time.
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
