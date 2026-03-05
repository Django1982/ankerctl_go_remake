package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type ipEntry struct {
	count       int
	windowStart time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*ipEntry
	limit   int
	window  time.Duration
}

// RateLimit creates an IP-based fixed-window rate-limiting middleware.
func RateLimit(requestsPerWindow int, window time.Duration) func(http.Handler) http.Handler {
	if requestsPerWindow <= 0 {
		requestsPerWindow = 100
	}
	if window <= 0 {
		window = time.Minute
	}

	rl := &rateLimiter{
		entries: make(map[string]*ipEntry),
		limit:   requestsPerWindow,
		window:  window,
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if shouldSkipRateLimit(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			now := time.Now()
			key := clientIP(r)

			rl.mu.Lock()
			rl.cleanupLocked(now)

			entry, ok := rl.entries[key]
			if !ok || now.Sub(entry.windowStart) >= rl.window {
				rl.entries[key] = &ipEntry{count: 1, windowStart: now}
				rl.mu.Unlock()
				next.ServeHTTP(w, r)
				return
			}

			if entry.count >= rl.limit {
				retryAfter := int(rl.window.Seconds()) - int(now.Sub(entry.windowStart).Seconds())
				if retryAfter < 1 {
					retryAfter = 1
				}
				rl.mu.Unlock()

				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Too many requests"})
				return
			}

			entry.count++
			rl.mu.Unlock()
			next.ServeHTTP(w, r)
		})
	}
}

func shouldSkipRateLimit(path string) bool {
	return strings.HasPrefix(path, "/static/") || path == "/api/health"
}

func (rl *rateLimiter) cleanupLocked(now time.Time) {
	threshold := rl.window * 2
	for ip, entry := range rl.entries {
		if now.Sub(entry.windowStart) > threshold {
			delete(rl.entries, ip)
		}
	}
}

func clientIP(r *http.Request) string {
	if trustProxyEnabled() {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[0])
		}
		if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
			return xrip
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func trustProxyEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("ANKERCTL_TRUST_PROXY")))
	return v == "1" || v == "true" || v == "yes"
}

func isWebsocketPath(path string) bool {
	return strings.HasPrefix(path, "/ws/")
}
