package middleware

import "net/http"

// SecurityHeaders adds security-related response headers to each HTTP response.
// Header set is identical to the Python after_request hook in web/__init__.py:
//
//	X-Content-Type-Options, X-Frame-Options, Referrer-Policy, Server
//
// X-XSS-Protection is intentionally omitted: it is deprecated by all major browsers
// and was never included in the Python reference implementation.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Server", "ankerctl")
		next.ServeHTTP(w, r)
	})
}
