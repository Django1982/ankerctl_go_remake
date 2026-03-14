package handler

import (
	"encoding/json"
	"net/http"

	"github.com/django1982/ankerctl/internal/service"
)

const (
	zOffsetMinMM = -10.0
	zOffsetMaxMM = 10.0
)

// ZOffsetGet returns the current Z-offset in millimeters.
// GET /api/printer/z-offset
func (h *Handler) ZOffsetGet(w http.ResponseWriter, r *http.Request) {
	mqtt, ok := h.mqttQueue()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "MQTT service unavailable")
		return
	}

	offset, known := mqtt.ZOffsetMM()
	if !known {
		h.writeError(w, http.StatusServiceUnavailable, "Z-offset not yet received from printer (no ct=1021)")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"z_offset_mm": offset,
	})
}

// ZOffsetSet sets the Z-offset to an absolute value in millimeters.
// POST /api/printer/z-offset
// Body: {"z_offset_mm": 0.15}
func (h *Handler) ZOffsetSet(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ZOffsetMM *float64 `json:"z_offset_mm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if payload.ZOffsetMM == nil {
		h.writeError(w, http.StatusBadRequest, "Missing required field: z_offset_mm")
		return
	}

	target := *payload.ZOffsetMM
	if target < zOffsetMinMM || target > zOffsetMaxMM {
		h.writeError(w, http.StatusBadRequest, "z_offset_mm must be between -10.0 and +10.0")
		return
	}

	mqtt, err := h.borrowMqttQueue()
	if err != nil {
		h.writeError(w, http.StatusServiceUnavailable, "MQTT service unavailable")
		return
	}
	defer h.svc.Return("mqttqueue")

	if err := mqtt.SetZOffset(r.Context(), target); err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	offset, _ := mqtt.ZOffsetMM()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"z_offset_mm": offset,
	})
}

// ZOffsetNudge adjusts the Z-offset by a relative delta in millimeters.
// POST /api/printer/z-offset/nudge
// Body: {"delta_mm": 0.01}
func (h *Handler) ZOffsetNudge(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		DeltaMM *float64 `json:"delta_mm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if payload.DeltaMM == nil {
		h.writeError(w, http.StatusBadRequest, "Missing required field: delta_mm")
		return
	}

	delta := *payload.DeltaMM
	if delta < zOffsetMinMM || delta > zOffsetMaxMM {
		h.writeError(w, http.StatusBadRequest, "delta_mm must be between -10.0 and +10.0")
		return
	}

	mqtt, err := h.borrowMqttQueue()
	if err != nil {
		h.writeError(w, http.StatusServiceUnavailable, "MQTT service unavailable")
		return
	}
	defer h.svc.Return("mqttqueue")

	if err := mqtt.NudgeZOffset(r.Context(), delta); err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	offset, _ := mqtt.ZOffsetMM()
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":      "ok",
		"z_offset_mm": offset,
	})
}

// borrowMqttQueue borrows the mqttqueue service via the ServiceManager.
// The caller must defer h.svc.Return("mqttqueue").
func (h *Handler) borrowMqttQueue() (*service.MqttQueue, error) {
	svc, err := h.svc.Borrow("mqttqueue")
	if err != nil {
		return nil, err
	}
	mqtt, ok := svc.(*service.MqttQueue)
	if !ok {
		h.svc.Return("mqttqueue")
		return nil, err
	}
	return mqtt, nil
}
