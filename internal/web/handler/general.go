package handler

import (
	"fmt"
	"net/http"
	"os"
)

// Root serves the web UI placeholder.
func (h *Handler) Root(w http.ResponseWriter, r *http.Request) {
	cfg, _ := h.loadConfig()
	printer, activeIdx, locked := h.activePrinter(cfg)

	data := TemplateData{
		ActivePrinterIndex: activeIdx,
		PrinterIndexLocked: locked,
		Configure:          cfg != nil && cfg.IsConfigured(),
		DebugMode:          h.devMode,
		VideoSupported:     true, // Default to true, can be refined based on model
		CountryCodes:       countryCodes,
	}

	if cfg != nil {
		data.Printers = cfg.Printers
		data.Printer = printer
		data.UploadRateMbps = cfg.UploadRateMbps
		if cfg.Account != nil {
			data.ConfigExistingEmail = cfg.Account.Email
			data.CurrentCountry = cfg.Account.Country
		}
	}

	if h.cfg != nil {
		data.LoginFilePath = h.cfg.ConfigDir()
	}

	if err := h.render(w, "base.html", data); err != nil {
		h.log.Error("render root", "error", err)
		h.writeError(w, http.StatusInternalServerError, "rendering failed")
	}
}

// Health is a lightweight liveness endpoint.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Version returns the API version payload (OctoPrint-compatible shape).
func (h *Handler) Version(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"api": "0.1", "server": "1.9.0", "text": "OctoPrint 1.9.0"})
}

// Video streams camera output; phase-10 keeps this as explicit TODO.
func (h *Handler) Video(w http.ResponseWriter, _ *http.Request) {
	h.writeError(w, http.StatusNotImplemented, "video stream not implemented")
}

// Snapshot captures a JPEG from VideoQueue and serves it as attachment.
func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
	vq, ok := h.videoQueue()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "video service not available")
		return
	}
	tmp, err := os.CreateTemp("", "ankerctl_snapshot_*.jpg")
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to create temp file")
		return
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(path)

	if err := vq.CaptureSnapshot(r.Context(), path); err != nil {
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("snapshot failed: %v", err))
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=ankerctl_snapshot.jpg")
	http.ServeFile(w, r, path)
}
