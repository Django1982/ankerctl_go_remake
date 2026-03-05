package handler

import (
	"encoding/json"
	"net/http"

	"github.com/django1982/ankerctl/internal/model"
)

// SettingsTimelapseGet returns timelapse settings.
func (h *Handler) SettingsTimelapseGet(w http.ResponseWriter, _ *http.Request) {
	cfg, err := h.loadConfig()
	if err != nil || cfg == nil {
		h.writeError(w, http.StatusBadRequest, "No printers configured")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"timelapse": cfg.Timelapse})
}

// SettingsTimelapseUpdate updates timelapse settings.
func (h *Handler) SettingsTimelapseUpdate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	tlPayload := payload
	if raw, ok := payload["timelapse"]; ok {
		m, ok := raw.(map[string]any)
		if !ok {
			h.writeError(w, http.StatusBadRequest, "Invalid timelapse payload")
			return
		}
		tlPayload = m
	}

	var updated model.TimelapseConfig
	err := h.cfg.Modify(func(cfg *model.Config) (*model.Config, error) {
		if cfg == nil {
			return cfg, nil
		}
		updated = cfg.Timelapse
		mergeIntoStruct(&updated, tlPayload)
		cfg.Timelapse = updated
		return cfg, nil
	})
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to update timelapse settings")
		return
	}

	if tl, ok := h.timelapse(); ok {
		printerSN := ""
		if cfg, err := h.loadConfig(); err == nil {
			if p, _, _ := h.activePrinter(cfg); p != nil {
				printerSN = p.SN
			}
		}
		tl.Configure(updated, printerSN)
	}

	h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "timelapse": updated})
}

// SettingsMQTTGet returns HomeAssistant MQTT settings.
func (h *Handler) SettingsMQTTGet(w http.ResponseWriter, _ *http.Request) {
	cfg, err := h.loadConfig()
	if err != nil || cfg == nil {
		h.writeError(w, http.StatusBadRequest, "No printers configured")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"home_assistant": cfg.HomeAssistant})
}

// SettingsMQTTUpdate updates HomeAssistant MQTT settings.
func (h *Handler) SettingsMQTTUpdate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	haPayload := payload
	if raw, ok := payload["home_assistant"]; ok {
		m, ok := raw.(map[string]any)
		if !ok {
			h.writeError(w, http.StatusBadRequest, "Invalid home_assistant payload")
			return
		}
		haPayload = m
	}

	var updated model.HomeAssistantConfig
	err := h.cfg.Modify(func(cfg *model.Config) (*model.Config, error) {
		if cfg == nil {
			return cfg, nil
		}
		updated = cfg.HomeAssistant
		mergeIntoStruct(&updated, haPayload)
		cfg.HomeAssistant = updated
		return cfg, nil
	})
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to update mqtt settings")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "home_assistant": updated})
}
