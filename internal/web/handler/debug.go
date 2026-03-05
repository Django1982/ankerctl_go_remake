package handler

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (h *Handler) ensureDevMode(w http.ResponseWriter) bool {
	if h.devMode {
		return true
	}
	http.NotFound(w, &http.Request{})
	return false
}

// DebugState returns a snapshot of mqttqueue state.
func (h *Handler) DebugState(w http.ResponseWriter, _ *http.Request) {
	if !h.devMode {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	mqtt, ok := h.mqttQueue()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "Service unavailable")
		return
	}
	h.writeJSON(w, http.StatusOK, mqtt.SnapshotState())
}

// DebugConfig toggles debug flags.
func (h *Handler) DebugConfig(w http.ResponseWriter, r *http.Request) {
	if !h.devMode {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	var payload struct {
		DebugLogging *bool `json:"debug_logging"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if payload.DebugLogging != nil {
		if mqtt, ok := h.mqttQueue(); ok {
			mqtt.SetDebugLogging(*payload.DebugLogging)
		}
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DebugSimulate injects synthetic events.
func (h *Handler) DebugSimulate(w http.ResponseWriter, r *http.Request) {
	if !h.devMode {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	var payload struct {
		Type    string         `json:"type"`
		Payload map[string]any `json:"payload"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	if mqtt, ok := h.mqttQueue(); ok {
		mqtt.SimulateEvent(payload.Type, payload.Payload)
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// DebugLogsList lists log files.
func (h *Handler) DebugLogsList(w http.ResponseWriter, _ *http.Request) {
	if !h.devMode {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	logDir := strings.TrimSpace(os.Getenv("ANKERCTL_LOG_DIR"))
	if logDir == "" {
		logDir = "/logs"
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		h.writeJSON(w, http.StatusOK, map[string]any{"files": []string{}})
		return
	}
	files := make([]string, 0)
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(strings.ToLower(e.Name()), ".log") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	h.writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

// DebugLogsContent returns tail of log file.
func (h *Handler) DebugLogsContent(w http.ResponseWriter, r *http.Request) {
	if !h.devMode {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	filename := chi.URLParam(r, "filename")
	if filename == "" || strings.Contains(filename, "..") || strings.ContainsAny(filename, `/\\`) || filepath.Base(filename) != filename {
		h.writeError(w, http.StatusBadRequest, "Invalid filename")
		return
	}
	logDir := strings.TrimSpace(os.Getenv("ANKERCTL_LOG_DIR"))
	if logDir == "" {
		logDir = "/logs"
	}
	path := filepath.Join(logDir, filename)
	realLogDir, _ := filepath.Abs(logDir)
	realPath, err := filepath.Abs(path)
	if err != nil || !strings.HasPrefix(realPath, realLogDir+string(os.PathSeparator)) {
		h.writeError(w, http.StatusBadRequest, "Invalid filename")
		return
	}
	data, err := os.ReadFile(realPath)
	if err != nil {
		h.writeError(w, http.StatusNotFound, "File not found")
		return
	}
	linesLimit := 500
	if raw := r.URL.Query().Get("lines"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			linesLimit = v
		}
	}
	content := tailLines(string(data), linesLimit)
	h.writeJSON(w, http.StatusOK, map[string]any{"filename": filename, "content": content})
}

func tailLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// DebugServices returns registered services with state and refs.
func (h *Handler) DebugServices(w http.ResponseWriter, _ *http.Request) {
	if !h.devMode {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	if h.svc == nil {
		h.writeJSON(w, http.StatusOK, map[string]any{"services": map[string]any{}})
		return
	}
	svcs := h.svc.ServicesSnapshot()
	refs := h.svc.RefsSnapshot()
	result := make(map[string]any, len(svcs))
	for name, svc := range svcs {
		result[name] = map[string]any{
			"state": int(svc.State()),
			"refs":  refs[name],
			"type":  "service",
		}
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"services": result})
}

// DebugServiceRestart triggers async restart for a named service.
func (h *Handler) DebugServiceRestart(w http.ResponseWriter, r *http.Request) {
	if !h.devMode {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	name := chi.URLParam(r, "name")
	svc, ok := h.serviceByName(name)
	if !ok {
		h.writeError(w, http.StatusNotFound, "Unknown service: "+name)
		return
	}
	go svc.Restart()
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "restarting"})
}

// DebugServiceTest runs service probe (currently only ppppservice).
func (h *Handler) DebugServiceTest(w http.ResponseWriter, r *http.Request) {
	if !h.devMode {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	name := chi.URLParam(r, "name")
	if name != "pppp" && name != "ppppservice" {
		h.writeError(w, http.StatusBadRequest, "Test not supported for service '"+name+"'")
		return
	}
	if _, ok := h.serviceByName("ppppservice"); ok {
		h.writeJSON(w, http.StatusOK, map[string]string{"result": "ok"})
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"result": "fail"})
}
