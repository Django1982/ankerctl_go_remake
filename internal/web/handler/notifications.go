package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/django1982/ankerctl/internal/model"
	"github.com/django1982/ankerctl/internal/notifications"
)

// NotificationsGet returns apprise settings.
func (h *Handler) NotificationsGet(w http.ResponseWriter, _ *http.Request) {
	cfg, err := h.loadConfig()
	if err != nil || cfg == nil {
		h.writeError(w, http.StatusBadRequest, "No printers configured")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"apprise": cfg.Notifications.Apprise})
}

// NotificationsUpdate updates notification settings.
func (h *Handler) NotificationsUpdate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	apprisePayload := payload
	if raw, ok := payload["apprise"]; ok {
		m, ok := raw.(map[string]any)
		if !ok {
			h.writeError(w, http.StatusBadRequest, "Invalid apprise payload")
			return
		}
		apprisePayload = m
	}

	if h.cfg == nil {
		h.writeError(w, http.StatusServiceUnavailable, "config manager unavailable")
		return
	}
	var updated model.AppriseConfig
	err := h.cfg.Modify(func(cfg *model.Config) (*model.Config, error) {
		if cfg == nil {
			return cfg, nil
		}
		updated = cfg.Notifications.Apprise
		mergeIntoStruct(&updated, apprisePayload)
		cfg.Notifications.Apprise = updated
		return cfg, nil
	})
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to update settings")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "apprise": updated})
}

// NotificationsTest sends a real Apprise test message, optionally using payload overrides.
func (h *Handler) NotificationsTest(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && err != io.EOF {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	var apprisePayload map[string]any
	if payload != nil {
		if raw, ok := payload["apprise"]; ok {
			m, ok := raw.(map[string]any)
			if !ok {
				h.writeError(w, http.StatusBadRequest, "Invalid apprise payload")
				return
			}
			apprisePayload = m
		} else {
			apprisePayload = payload
		}
	}

	cfg, err := h.loadConfig()
	if err != nil || cfg == nil {
		h.writeError(w, http.StatusBadRequest, "No printers configured")
		return
	}

	appriseCfg := cfg.Notifications.Apprise
	if apprisePayload != nil {
		mergeIntoStruct(&appriseCfg, apprisePayload)
	}

	var snap notifications.SnapshotCapturer
	if vq, ok := h.videoQueue(); ok {
		snap = vq
	}

	ok, message := notifications.SendTestNotification(r.Context(), appriseCfg, snap)
	if !ok {
		h.writeError(w, http.StatusBadRequest, message)
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "message": message})
}
