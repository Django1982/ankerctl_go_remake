package middleware

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// WebAuthState provides the runtime state required by the browser login gate.
//
// This is deliberately independent of APIKey()/X-Api-Key auth: the API key
// authenticates machines (slicers, scripts) making individual write
// requests, while this gate authenticates a human sitting at a browser for
// the whole UI session. The two mechanisms stay orthogonal on purpose so
// that enabling one never silently changes the other's behavior.
type WebAuthState interface {
	WebAuthEnabled() bool
	VerifyWebCredentials(username, password string) bool
	SessionManager() *SessionManager
	APIKey() string
}

// webAuthExemptPaths are reachable without a browser session, even when web
// login is enabled.
var webAuthExemptPaths = map[string]bool{
	"/login":       true,
	"/logout":      true,
	"/api/health":  true,
	"/api/version": true,
}

// WebAuthGate redirects unauthenticated browser requests to a login page
// when ANKERCTL_WEB_PASSWORD_HASH is configured. It intentionally does not
// replace API-key auth (see WebAuthState doc comment) - a request carrying
// a valid X-Api-Key is passed straight through, so slicers and scripts are
// unaffected either way.
//
// Why this exists instead of HTTP Basic Auth in front of the proxy: this
// app keeps several long-lived WebSocket connections open (/ws/mqtt,
// /ws/ctrl, /ws/pppp-state, /ws/video) and polls a number of JSON endpoints
// on every tab switch. Browsers cannot attach a cached Basic Auth
// credential to a WebSocket handshake the way they can to a normal
// same-origin request, and *any* 401 response carrying a WWW-Authenticate
// header - even one coming back on a background XHR/WS handshake, not just
// a top-level navigation - makes the browser pop its native credential
// dialog again. With this many background requests firing on every tab
// change, that adds up to a login prompt on practically every interaction.
// A real session cookie set after a normal form POST doesn't have that
// problem: it's just an ordinary same-origin cookie, sent automatically on
// every request including WebSocket upgrades.
func WebAuthGate(state WebAuthState) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !state.WebAuthEnabled() {
				next.ServeHTTP(w, r)
				return
			}

			if webAuthExemptPaths[r.URL.Path] || strings.HasPrefix(r.URL.Path, "/static/") {
				next.ServeHTTP(w, r)
				return
			}

			sm := state.SessionManager()
			if sm != nil && sm.IsAuthenticated(r) {
				next.ServeHTTP(w, r)
				return
			}

			if apiKey := state.APIKey(); apiKey != "" && secureEquals(r.Header.Get("X-Api-Key"), apiKey) {
				next.ServeHTTP(w, r)
				return
			}

			if r.Method == http.MethodGet || r.Method == http.MethodHead {
				next := url.QueryEscape(r.URL.RequestURI())
				http.Redirect(w, r, "/login?next="+next, http.StatusFound)
				return
			}

			writeJSONError(w, http.StatusUnauthorized, "Unauthorized. Please log in at /login.")
		})
	}
}

// loginAttempts tracks failed login attempts per client IP for lockout.
type loginAttempts struct {
	mu          sync.Mutex
	failures    map[string]int
	lockedUntil map[string]time.Time
	maxAttempts int
	lockoutFor  time.Duration
}

func newLoginAttempts(maxAttempts int, lockoutFor time.Duration) *loginAttempts {
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if lockoutFor <= 0 {
		lockoutFor = 5 * time.Minute
	}
	return &loginAttempts{
		failures:    make(map[string]int),
		lockedUntil: make(map[string]time.Time),
		maxAttempts: maxAttempts,
		lockoutFor:  lockoutFor,
	}
}

// lockedRemaining returns the remaining lockout duration for key, or 0 if
// it is not currently locked out.
func (a *loginAttempts) lockedRemaining(key string) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	until, ok := a.lockedUntil[key]
	if !ok {
		return 0
	}
	remaining := time.Until(until)
	if remaining <= 0 {
		delete(a.lockedUntil, key)
		delete(a.failures, key)
		return 0
	}
	return remaining
}

func (a *loginAttempts) recordFailure(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures[key]++
	if a.failures[key] >= a.maxAttempts {
		a.lockedUntil[key] = time.Now().Add(a.lockoutFor)
	}
}

func (a *loginAttempts) recordSuccess(key string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.failures, key)
	delete(a.lockedUntil, key)
}

// remainingAttempts returns how many more failures key can accrue before
// being locked out.
func (a *loginAttempts) remainingAttempts(key string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	remaining := a.maxAttempts - a.failures[key]
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// LoginHandlers serves the login form, processes submissions, and handles
// logout. It holds the attempt limiter so state persists across requests
// for the life of the process.
type LoginHandlers struct {
	state    WebAuthState
	attempts *loginAttempts
	render   func(w http.ResponseWriter, next string, errMsg string, status int)
}

// NewLoginHandlers constructs login/logout handlers.
//
// maxAttempts/lockoutFor configure brute-force lockout: after maxAttempts
// consecutive failures from the same client IP, further attempts from that
// IP are rejected for lockoutFor, regardless of whether the password is
// correct. The counter resets on a successful login or once the lockout
// expires.
func NewLoginHandlers(state WebAuthState, maxAttempts int, lockoutFor time.Duration, render func(w http.ResponseWriter, next string, errMsg string, status int)) *LoginHandlers {
	return &LoginHandlers{
		state:    state,
		attempts: newLoginAttempts(maxAttempts, lockoutFor),
		render:   render,
	}
}

// ShowLogin renders the login form.
func (h *LoginHandlers) ShowLogin(w http.ResponseWriter, r *http.Request) {
	next := sanitizeNext(r.URL.Query().Get("next"))
	h.render(w, next, "", http.StatusOK)
}

// HandleLogin validates submitted credentials and, on success, sets the
// session cookie shared with the rest of the auth stack.
func (h *LoginHandlers) HandleLogin(w http.ResponseWriter, r *http.Request) {
	next := sanitizeNext(r.URL.Query().Get("next"))
	ip := clientIP(r)

	if remaining := h.attempts.lockedRemaining(ip); remaining > 0 {
		h.render(w, next, "Too many failed attempts. Try again in "+formatDuration(remaining)+".", http.StatusTooManyRequests)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.render(w, next, "Invalid form submission.", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	if !h.state.VerifyWebCredentials(username, password) {
		h.attempts.recordFailure(ip)
		remaining := h.attempts.remainingAttempts(ip)
		msg := "Incorrect username or password."
		if remaining > 0 && remaining < h.attempts.maxAttempts {
			msg += " " + strconv.Itoa(remaining) + " attempt(s) remaining before lockout."
		}
		h.render(w, next, msg, http.StatusUnauthorized)
		return
	}

	h.attempts.recordSuccess(ip)
	sm := h.state.SessionManager()
	if sm != nil {
		sm.SetAuthenticated(w, r, true)
	}
	http.Redirect(w, r, next, http.StatusFound)
}

// HandleLogout clears the session cookie.
func (h *LoginHandlers) HandleLogout(w http.ResponseWriter, r *http.Request) {
	sm := h.state.SessionManager()
	if sm != nil {
		sm.SetAuthenticated(w, r, false)
	}
	http.Redirect(w, r, "/login", http.StatusFound)
}

// sanitizeNext only allows same-origin relative paths, to avoid turning the
// "next" redirect target into an open redirect.
func sanitizeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, "\\") {
		return "/"
	}
	if u, err := url.Parse(next); err != nil || u.IsAbs() || u.Host != "" {
		return "/"
	}
	return next
}

func formatDuration(d time.Duration) string {
	return d.Round(time.Second).String()
}

// VerifyBcryptPassword compares a plaintext password against a bcrypt hash
// in the same format Caddy's `caddy hash-password` / basic_auth produces,
// so the exact same hash can be reused for both.
func VerifyBcryptPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
