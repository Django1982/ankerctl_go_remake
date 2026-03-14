package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/django1982/ankerctl/internal/logging"
	"github.com/django1982/ankerctl/internal/model"
	ppppclient "github.com/django1982/ankerctl/internal/pppp/client"
	"github.com/django1982/ankerctl/internal/service"
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
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
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
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if mqtt, ok := h.mqttQueue(); ok {
		mqtt.SimulateEvent(payload.Type, payload.Payload)
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

const liveLogFilename = "live.log"

// DebugLogsList lists log files. It always includes a virtual "live.log" entry
// backed by the in-memory ring buffer, plus any real .log files found in
// the resolved log directory (ANKERCTL_LOG_DIR or /logs if it exists).
func (h *Handler) DebugLogsList(w http.ResponseWriter, _ *http.Request) {
	if !h.devMode {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}

	// Always expose the live ring buffer as the first entry.
	files := []string{liveLogFilename}

	if h.logDir != "" {
		if entries, err := os.ReadDir(h.logDir); err == nil {
			var diskFiles []string
			for _, e := range entries {
				if e.Type().IsRegular() && strings.HasSuffix(strings.ToLower(e.Name()), ".log") {
					diskFiles = append(diskFiles, e.Name())
				}
			}
			sort.Strings(diskFiles)
			files = append(files, diskFiles...)
		}
	}

	resp := map[string]any{"files": files}
	if h.logDir == "" {
		resp["warning"] = "No log directory configured (set ANKERCTL_LOG_DIR)"
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// DebugLogsContent returns the tail of a log file. The special filename
// "live.log" is served from the in-memory ring buffer; all other names are
// resolved relative to ANKERCTL_LOG_DIR.
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

	linesLimit := 500
	if raw := r.URL.Query().Get("lines"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			linesLimit = v
		}
	}

	// Serve the virtual "live.log" from the ring buffer.
	if filename == liveLogFilename {
		var content string
		if h.logRing != nil {
			content = strings.Join(h.logRing.Tail(linesLimit), "\n")
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"filename": filename, "content": content})
		return
	}

	if h.logDir == "" {
		h.writeError(w, http.StatusNotFound, "No log directory configured (set ANKERCTL_LOG_DIR)")
		return
	}
	path := filepath.Join(h.logDir, filename)
	realLogDir, _ := filepath.Abs(h.logDir)
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

// runStateName maps a RunState integer to the string expected by the JS UI.
func runStateName(s service.RunState) string {
	switch s {
	case service.StateStopped:
		return "Stopped"
	case service.StateStarting:
		return "Starting"
	case service.StateRunning:
		return "Running"
	case service.StateStopping:
		return "Stopping"
	default:
		return "Unknown"
	}
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

	type wantedGetter interface {
		Wanted() bool
	}

	for name, svc := range svcs {
		entry := map[string]any{
			"state": runStateName(svc.State()),
			"refs":  refs[name],
			"type":  "service",
		}
		if wg, ok := svc.(wantedGetter); ok {
			entry["wanted"] = wg.Wanted()
		}
		result[name] = entry
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"services": result})
}

// DebugVideoStats returns live video runtime metrics (dev mode only).
func (h *Handler) DebugVideoStats(w http.ResponseWriter, _ *http.Request) {
	if !h.devMode {
		h.writeError(w, http.StatusNotFound, "not found")
		return
	}
	vq, ok := h.videoQueue()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "videoqueue unavailable")
		return
	}
	h.writeJSON(w, http.StatusOK, vq.RuntimeStats())
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

// DiscoverPrinterIP runs a LAN broadcast discovery for the active printer's
// DUID, returns the discovered IP address, and persists it in config.
// Route: POST /api/debug/pppp/discover (devMode only).
func (h *Handler) DiscoverPrinterIP(w http.ResponseWriter, r *http.Request) {
	if !h.ensureDevMode(w) {
		return
	}

	cfg, err := h.loadConfig()
	if err != nil || cfg == nil {
		h.writeError(w, http.StatusServiceUnavailable, "config unavailable")
		return
	}

	printer, _, _ := h.activePrinter(cfg)
	if printer == nil {
		h.writeError(w, http.StatusPreconditionFailed, "no printer configured")
		return
	}
	if printer.P2PDUID == "" {
		h.writeError(w, http.StatusPreconditionFailed, "printer has no P2P DUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	start := time.Now()
	ip, err := ppppclient.DiscoverLANIP(ctx, printer.P2PDUID)
	elapsed := time.Since(start)

	if err != nil {
		slog.Warn("LAN discovery failed", "duid", logging.Redact(map[string]any{"p2p_duid": printer.P2PDUID})["p2p_duid"], "error", err)
		h.writeJSON(w, http.StatusOK, map[string]any{
			"error":      "timeout after 5s",
			"elapsed_ms": elapsed.Milliseconds(),
		})
		return
	}

	ipStr := ip.String()
	slog.Info("LAN discovery succeeded", "ip", ipStr, "elapsed_ms", elapsed.Milliseconds())

	// Persist discovered IP into config and DB cache.
	if h.cfg != nil {
		saveErr := h.cfg.Modify(func(saved *model.Config) (*model.Config, error) {
			if saved == nil {
				return nil, nil
			}
			_, idx, _ := h.activePrinter(saved)
			if idx >= 0 && idx < len(saved.Printers) {
				saved.Printers[idx].IPAddr = ipStr
			}
			return saved, nil
		})
		if saveErr != nil {
			slog.Warn("discover: could not persist IP to config", "error", saveErr)
		}
	}
	if h.db != nil && printer.SN != "" {
		if dbErr := h.db.SetPrinterIP(printer.SN, ipStr); dbErr != nil {
			slog.Warn("discover: could not cache IP in db", "error", dbErr)
		}
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"ip":         ipStr,
		"duid":       printer.P2PDUID,
		"elapsed_ms": elapsed.Milliseconds(),
	})
}

// ppppServiceConn is the subset of PPPPService used by the reconnect handler.
type ppppServiceConn interface {
	service.Service
	IsConnected() bool
}

// PPPPReconnect restarts the ppppservice, waits up to 6 seconds for the
// PPPP handshake to complete, then returns the connection result and only
// the log lines that were emitted during this attempt.
// Route: POST /api/debug/pppp/reconnect (devMode only).
func (h *Handler) PPPPReconnect(w http.ResponseWriter, r *http.Request) {
	if !h.ensureDevMode(w) {
		return
	}
	rawSvc, ok := h.serviceByName("ppppservice")
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "ppppservice not registered")
		return
	}
	pppp, ok := rawSvc.(ppppServiceConn)
	if !ok {
		h.writeError(w, http.StatusInternalServerError, "ppppservice type mismatch")
		return
	}

	// Snapshot ring-buffer size before restart so we can return only NEW lines.
	var preLen int
	if h.logRing != nil {
		preLen = len(h.logRing.Lines())
	}

	slog.Info("debug: manual PPPP reconnect triggered")
	pppp.Restart()

	// Poll up to 6 s for the PPPP handshake to reach Connected (or fail).
	deadline := time.Now().Add(6 * time.Second)
	connected := false
	for time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		if pppp.IsConnected() {
			connected = true
			break
		}
		// Stop early if the service crashed without connecting.
		if st := runStateName(pppp.State()); st == "Stopped" {
			break
		}
	}

	// Collect only lines added since the restart.
	var newLines []string
	if h.logRing != nil {
		all := h.logRing.Lines()
		if preLen < len(all) {
			newLines = all[preLen:]
		}
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"connected": connected,
		"state":     runStateName(pppp.State()),
		"log":       newLines,
	})
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
