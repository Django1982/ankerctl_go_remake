package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
)

// AccessLogger logs request metadata with structured fields.
func AccessLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"duration", time.Since(start),
				"ip", remoteIP(r),
				"request_id", chimw.GetReqID(r.Context()),
			}

			if strings.HasPrefix(r.URL.Path, "/static/") {
				logger.Debug("HTTP request", attrs...)
				return
			}

			switch status := ww.Status(); {
			case status >= 500:
				logger.Error("HTTP request", attrs...)
			case status >= 400:
				logger.Warn("HTTP request", attrs...)
			default:
				logger.Info("HTTP request", attrs...)
			}
		})
	}
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
