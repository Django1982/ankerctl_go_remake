package middleware

import (
	"encoding/json"
	"net/http"
	"strings"
)

var printerControlPrefixes = []string{
	"/api/printer/",
	"/api/files/",
	"/api/filaments",
}

// DeviceState provides server runtime state required for device checks.
type DeviceState interface {
	IsLoggedIn() bool
	IsUnsupportedDevice() bool
}

// RequirePrinter returns 503 for printer-control paths when no printer is configured.
func RequirePrinter(state DeviceState) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/static/") {
				next.ServeHTTP(w, r)
				return
			}
			if !isPrinterControlPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			if state.IsLoggedIn() {
				next.ServeHTTP(w, r)
				return
			}
			writeJSONError(w, http.StatusServiceUnavailable, "No printer configured. Import configuration first.")
		})
	}
}

// BlockUnsupportedDevice returns 503 for printer-control paths if active device is unsupported.
func BlockUnsupportedDevice(state DeviceState) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/static/") {
				next.ServeHTTP(w, r)
				return
			}
			if !isPrinterControlPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			if !state.IsUnsupportedDevice() {
				next.ServeHTTP(w, r)
				return
			}
			writeJSONError(w, http.StatusServiceUnavailable, "Unsupported printer model for this operation.")
		})
	}
}

func isPrinterControlPath(path string) bool {
	for _, prefix := range printerControlPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
