package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/django1982/ankerctl/internal/model"
)

// ConfigUpload imports config JSON from multipart upload.
func (h *Handler) ConfigUpload(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("login_file")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "No file found")
		return
	}
	defer file.Close()

	var cfg model.Config
	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid config json")
		return
	}
	if h.cfg == nil {
		h.writeError(w, http.StatusServiceUnavailable, "config manager unavailable")
		return
	}
	if err := h.cfg.Save(&cfg); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to persist config")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ConfigLogin performs cloud login and config bootstrap.
func (h *Handler) ConfigLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	if r.FormValue("login_email") == "" || r.FormValue("login_password") == "" || r.FormValue("login_country") == "" {
		h.writeError(w, http.StatusBadRequest, "missing login parameters")
		return
	}
	// TODO(phase-13): Wire internal/httpapi login flow. Kept as explicit stub.
	h.writeError(w, http.StatusNotImplemented, "cloud login not implemented")
}

// ServerReload restarts all registered services.
func (h *Handler) ServerReload(w http.ResponseWriter, _ *http.Request) {
	if h.svc != nil {
		h.svc.RestartAll()
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// UploadRateUpdate updates config.upload_rate_mbps.
func (h *Handler) UploadRateUpdate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	rateRaw := r.FormValue("upload_rate_mbps")
	if rateRaw == "" {
		h.writeError(w, http.StatusBadRequest, "upload_rate_mbps missing")
		return
	}
	rate, err := strconv.Atoi(rateRaw)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "upload_rate_mbps must be an integer")
		return
	}

	valid := false
	for _, v := range model.UploadRateMbpsChoices {
		if v == rate {
			valid = true
			break
		}
	}
	if !valid {
		h.writeError(w, http.StatusBadRequest, "invalid upload_rate_mbps")
		return
	}

	if h.cfg == nil {
		h.writeError(w, http.StatusServiceUnavailable, "config manager unavailable")
		return
	}
	if err := h.cfg.Modify(func(cfg *model.Config) (*model.Config, error) {
		if cfg == nil {
			return nil, nil
		}
		cfg.UploadRateMbps = rate
		return cfg, nil
	}); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to update upload rate")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "upload_rate_mbps": rate})
}
