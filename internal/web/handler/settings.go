package handler

import (
	"encoding/json"
	"net/http"

	"github.com/django1982/ankerctl/internal/model"
)

// SettingsTimelapseGet returns timelapse settings.
func (h *Handler) SettingsTimelapseGet(w http.ResponseWriter, _ *http.Request) {
	cfg, _ := h.loadConfig()
	var tl model.TimelapseConfig
	if cfg != nil {
		tl = cfg.Timelapse
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"timelapse": tl})
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
	cfg, _ := h.loadConfig()
	var ha model.HomeAssistantConfig
	if cfg != nil {
		ha = cfg.HomeAssistant
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"home_assistant": ha})
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

	if ha, ok := h.homeAssistant(); ok {
		ha.Configure(updated)
	}

	h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "home_assistant": updated})
}

// SettingsAppearanceGet returns the appearance settings.
func (h *Handler) SettingsAppearanceGet(w http.ResponseWriter, _ *http.Request) {
	cfg, _ := h.loadConfig()
	var app model.AppearanceConfig
	if cfg != nil {
		app = cfg.Appearance
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"appearance": app})
}

// SettingsAppearanceUpdate updates the appearance settings.
func (h *Handler) SettingsAppearanceUpdate(w http.ResponseWriter, r *http.Request) {
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	appPayload := payload
	if raw, ok := payload["appearance"]; ok {
		m, ok := raw.(map[string]any)
		if !ok {
			h.writeError(w, http.StatusBadRequest, "Invalid appearance payload")
			return
		}
		appPayload = m
	}

	var updated model.AppearanceConfig
	err := h.cfg.Modify(func(cfg *model.Config) (*model.Config, error) {
		if cfg == nil {
			return cfg, nil
		}
		updated = cfg.Appearance
		mergeIntoStruct(&updated, appPayload)
		cfg.Appearance = updated
		return cfg, nil
	})
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to update appearance settings")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "appearance": updated})
}
