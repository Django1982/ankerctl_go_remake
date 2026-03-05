package handler

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func parseFilamentID(r *http.Request) (int64, error) {
	idRaw := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid filament id")
	}
	return id, nil
}

func sanitizeFilamentPayload(data map[string]any) {
	for _, key := range []string{"name", "brand", "material", "color", "notes", "seam_position"} {
		if v, ok := data[key].(string); ok {
			data[key] = html.EscapeString(v)
		}
	}
}

// FilamentList lists all filament profiles.
// Response shape matches Python: {"filaments": [...]}.
func (h *Handler) FilamentList(w http.ResponseWriter, _ *http.Request) {
	if h.db == nil {
		h.writeJSON(w, http.StatusOK, map[string]any{"filaments": []any{}})
		return
	}
	profiles, err := h.db.ListFilaments()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to list filaments")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"filaments": profiles})
}

// FilamentCreate creates a new profile.
func (h *Handler) FilamentCreate(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		h.writeError(w, http.StatusServiceUnavailable, "filament store unavailable")
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	sanitizeFilamentPayload(payload)
	profile, err := h.db.CreateFilament(payload)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.writeJSON(w, http.StatusCreated, profile)
}

// FilamentUpdate updates a profile by ID.
func (h *Handler) FilamentUpdate(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		h.writeError(w, http.StatusServiceUnavailable, "filament store unavailable")
		return
	}
	id, err := parseFilamentID(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}
	sanitizeFilamentPayload(payload)
	profile, err := h.db.UpdateFilament(id, payload)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if profile == nil {
		h.writeError(w, http.StatusNotFound, "Profile not found")
		return
	}
	h.writeJSON(w, http.StatusOK, profile)
}

// FilamentDelete deletes a profile.
func (h *Handler) FilamentDelete(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		h.writeError(w, http.StatusServiceUnavailable, "filament store unavailable")
		return
	}
	id, err := parseFilamentID(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	deleted, err := h.db.DeleteFilament(id)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to delete profile")
		return
	}
	if !deleted {
		h.writeError(w, http.StatusNotFound, "Profile not found")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// FilamentApply sends preheat gcode from selected profile.
func (h *Handler) FilamentApply(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		h.writeError(w, http.StatusServiceUnavailable, "filament store unavailable")
		return
	}
	id, err := parseFilamentID(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	profile, err := h.db.GetFilament(id)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to load profile")
		return
	}
	if profile == nil {
		h.writeError(w, http.StatusNotFound, "Profile not found")
		return
	}
	mqtt, ok := h.mqttQueue()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "Service unavailable")
		return
	}
	gcode := fmt.Sprintf("M104 S%d\nM140 S%d", profile.NozzleTempFirstLayer, profile.BedTempFirstLayer)
	if err := mqtt.SendGCode(r.Context(), gcode); err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "gcode": gcode})
}

// FilamentDuplicate duplicates a profile.
func (h *Handler) FilamentDuplicate(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		h.writeError(w, http.StatusServiceUnavailable, "filament store unavailable")
		return
	}
	id, err := parseFilamentID(r)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	profile, err := h.db.DuplicateFilament(id)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to duplicate profile")
		return
	}
	if profile == nil {
		h.writeError(w, http.StatusNotFound, "Profile not found")
		return
	}
	h.writeJSON(w, http.StatusCreated, profile)
}
