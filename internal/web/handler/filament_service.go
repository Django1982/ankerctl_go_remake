package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/django1982/ankerctl/internal/db"
	"github.com/django1982/ankerctl/internal/service"
)

// Filament service constants matching Python exactly.
const (
	filamentServiceDefaultLengthMM = 40.0
	filamentServiceMaxLengthMM     = 300.0
	filamentServiceFeedrateMMMin   = 240
	filamentServiceHeatTimeoutS    = 240.0
	filamentServiceHeatPollS       = 500 * time.Millisecond
	filamentServiceHeatToleranceC  = 5
)

// filamentSwapState holds the state of an in-progress filament swap.
type filamentSwapState struct {
	Token             string  `json:"token"`
	CreatedAt         int64   `json:"created_at"`
	UnloadProfileID   int64   `json:"unload_profile_id"`
	UnloadProfileName string  `json:"unload_profile_name"`
	LoadProfileID     int64   `json:"load_profile_id"`
	LoadProfileName   string  `json:"load_profile_name"`
	UnloadTempC       int     `json:"unload_temp_c"`
	LoadTempC         int     `json:"load_temp_c"`
	UnloadLengthMM    float64 `json:"unload_length_mm"`
	LoadLengthMM      float64 `json:"load_length_mm"`
}

// filamentSwapManager holds the mutex-protected swap state.
// It lives on the Handler so it's shared across requests.
var (
	filamentSwapMu    sync.Mutex
	filamentSwapValue *filamentSwapState
)

// filamentServiceTemp extracts the nozzle temperature from a filament profile,
// checking nozzle_temp_other_layer first, then first_layer. Returns error if 0.
func filamentServiceTemp(profile *db.FilamentProfile) (int, error) {
	temp := profile.NozzleTempOtherLayer
	if temp <= 0 {
		temp = profile.NozzleTempFirstLayer
	}
	if temp <= 0 {
		return 0, fmt.Errorf("Filament profile has no usable nozzle temperature")
	}
	return temp, nil
}

// filamentServiceLength parses and validates a length_mm value from a JSON payload.
func filamentServiceLength(payload map[string]any, key string) (float64, error) {
	raw, ok := payload[key]
	if !ok {
		return filamentServiceDefaultLengthMM, nil
	}
	var length float64
	switch v := raw.(type) {
	case float64:
		length = v
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0, fmt.Errorf("%s must be a number", key)
		}
		length = f
	default:
		return 0, fmt.Errorf("%s must be a number", key)
	}
	if length <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", key)
	}
	if length > filamentServiceMaxLengthMM {
		return 0, fmt.Errorf("%s must be <= %g", key, filamentServiceMaxLengthMM)
	}
	return math.Round(length*100) / 100, nil
}

// filamentServiceProfile looks up a filament profile by ID from the payload.
func (h *Handler) filamentServiceProfile(payload map[string]any, key string) (*db.FilamentProfile, error) {
	raw, ok := payload[key]
	if !ok {
		return nil, fmt.Errorf("%s must be an integer", key)
	}
	var id int64
	switch v := raw.(type) {
	case float64:
		id = int64(v)
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return nil, fmt.Errorf("%s must be an integer", key)
		}
		id = i
	default:
		return nil, fmt.Errorf("%s must be an integer", key)
	}
	if h.db == nil {
		return nil, fmt.Errorf("filament store unavailable")
	}
	profile, err := h.db.GetFilament(id)
	if err != nil {
		return nil, fmt.Errorf("failed to load filament profile")
	}
	if profile == nil {
		return nil, &lookupError{msg: fmt.Sprintf("Filament profile %d not found", id)}
	}
	return profile, nil
}

// lookupError represents a "not found" error (maps to HTTP 404).
type lookupError struct{ msg string }

func (e *lookupError) Error() string { return e.msg }

// runtimeError represents a conflict error (maps to HTTP 409).
type runtimeError struct{ msg string }

func (e *runtimeError) Error() string { return e.msg }

// timeoutError represents a timeout (maps to HTTP 504).
type timeoutError struct{ msg string }

func (e *timeoutError) Error() string { return e.msg }

// connectionError represents an unavailable service (maps to HTTP 503).
type connectionError struct{ msg string }

func (e *connectionError) Error() string { return e.msg }

// formatExtrusionMM formats a length_mm without trailing zeros.
func formatExtrusionMM(lengthMM float64) string {
	text := fmt.Sprintf("%.2f", lengthMM)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" || text == "-" {
		return "0"
	}
	return text
}

// buildFilamentMoveGcode builds the GCode for extrude/retract.
func buildFilamentMoveGcode(deltaMM float64) string {
	extrusion := formatExtrusionMM(deltaMM)
	return strings.Join([]string{
		"M83",
		"G92 E0",
		fmt.Sprintf("G1 E%s F%d", extrusion, filamentServiceFeedrateMMMin),
		"G92 E0",
		"M82",
		"M400",
	}, "\n")
}

// serializeFilamentSwapState produces the JSON-compatible map for a swap state.
func serializeFilamentSwapState(state *filamentSwapState) map[string]any {
	if state == nil {
		return map[string]any{"pending": false, "swap": nil}
	}
	return map[string]any{
		"pending": true,
		"swap": map[string]any{
			"token":                state.Token,
			"created_at":           state.CreatedAt,
			"unload_profile_id":    state.UnloadProfileID,
			"unload_profile_name":  state.UnloadProfileName,
			"load_profile_id":      state.LoadProfileID,
			"load_profile_name":    state.LoadProfileName,
			"unload_temp_c":        state.UnloadTempC,
			"load_temp_c":          state.LoadTempC,
			"unload_length_mm":     state.UnloadLengthMM,
			"load_length_mm":       state.LoadLengthMM,
		},
	}
}

// generateToken generates a random hex token of n bytes (2*n hex chars).
func generateToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// assertFilamentServiceReady checks MQTT is available and not printing.
func assertFilamentServiceReady(mqtt *service.MqttQueue) error {
	if mqtt == nil {
		return &connectionError{"MQTT service unavailable"}
	}
	if mqtt.IsPrinting() {
		return &runtimeError{"Filament service commands are blocked while a print is active"}
	}
	return nil
}

// waitForNozzle polls the nozzle temperature until it reaches target, with timeout.
func waitForNozzle(mqtt *service.MqttQueue, targetTempC int) (int, error) {
	deadline := time.Now().Add(time.Duration(filamentServiceHeatTimeoutS) * time.Second)
	var nextQuery time.Time
	lastTemp := mqtt.NozzleTemp()
	targetReady := targetTempC - filamentServiceHeatToleranceC

	for time.Now().Before(deadline) {
		now := time.Now()
		if now.After(nextQuery) || nextQuery.IsZero() {
			mqtt.RequestStatus()
			nextQuery = now.Add(2 * time.Second)
		}
		current := mqtt.NozzleTemp()
		if current != nil {
			lastTemp = current
			if *current >= targetReady {
				return *current, nil
			}
		}
		time.Sleep(filamentServiceHeatPollS)
	}

	lastStr := "unknown"
	if lastTemp != nil {
		lastStr = fmt.Sprintf("%d", *lastTemp)
	}
	return 0, &timeoutError{
		msg: fmt.Sprintf("Nozzle did not reach %d\u00b0C within %ds (last seen: %s\u00b0C)",
			targetTempC, int(filamentServiceHeatTimeoutS), lastStr),
	}
}

// writeFilamentServiceError maps typed errors to HTTP status codes (Python parity).
func (h *Handler) writeFilamentServiceError(w http.ResponseWriter, err error) {
	switch err.(type) {
	case *lookupError:
		h.writeError(w, http.StatusNotFound, err.Error())
	case *runtimeError:
		h.writeError(w, http.StatusConflict, err.Error())
	case *timeoutError:
		h.writeError(w, http.StatusGatewayTimeout, err.Error())
	case *connectionError:
		h.writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		// Default: treat as ValueError -> 400.
		h.writeError(w, http.StatusBadRequest, err.Error())
	}
}

// borrowMqttForFilament borrows the mqttqueue and asserts it's ready.
func (h *Handler) borrowMqttForFilament() (*service.MqttQueue, error) {
	svc, err := h.svc.Borrow("mqttqueue")
	if err != nil {
		return nil, &connectionError{"MQTT service unavailable"}
	}
	mqtt, ok := svc.(*service.MqttQueue)
	if !ok {
		h.svc.Return("mqttqueue")
		return nil, &connectionError{"MQTT service type mismatch"}
	}
	if err := assertFilamentServiceReady(mqtt); err != nil {
		h.svc.Return("mqttqueue")
		return nil, err
	}
	return mqtt, nil
}

func (h *Handler) returnMqtt() {
	h.svc.Return("mqttqueue")
}

// FilamentServiceSwapState returns the current swap state (GET, unprotected).
func (h *Handler) FilamentServiceSwapState(w http.ResponseWriter, _ *http.Request) {
	filamentSwapMu.Lock()
	state := filamentSwapValue
	filamentSwapMu.Unlock()
	h.writeJSON(w, http.StatusOK, serializeFilamentSwapState(state))
}

// FilamentServicePreheat heats the nozzle to the profile temperature.
func (h *Handler) FilamentServicePreheat(w http.ResponseWriter, r *http.Request) {
	payload := h.readJSONPayload(r)
	profile, err := h.filamentServiceProfile(payload, "profile_id")
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	tempC, err := filamentServiceTemp(profile)
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	gcode := fmt.Sprintf("M104 S%d", tempC)
	mqtt, err := h.borrowMqttForFilament()
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	defer h.returnMqtt()
	if err := mqtt.SendGCode(r.Context(), gcode); err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"action":       "preheat",
		"profile_id":   profile.ID,
		"profile_name": profile.Name,
		"target_temp_c": tempC,
		"gcode":        gcode,
	})
}

// FilamentServiceMove extrudes or retracts filament with auto-heat.
func (h *Handler) FilamentServiceMove(w http.ResponseWriter, r *http.Request) {
	payload := h.readJSONPayload(r)
	action, _ := payload["action"].(string)
	action = strings.ToLower(strings.TrimSpace(action))
	if action != "extrude" && action != "retract" {
		h.writeError(w, http.StatusBadRequest, "action must be 'extrude' or 'retract'")
		return
	}

	profile, err := h.filamentServiceProfile(payload, "profile_id")
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	tempC, err := filamentServiceTemp(profile)
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	lengthMM, err := filamentServiceLength(payload, "length_mm")
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	deltaMM := lengthMM
	if action == "retract" {
		deltaMM = -lengthMM
	}
	gcode := buildFilamentMoveGcode(deltaMM)

	mqtt, err := h.borrowMqttForFilament()
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	defer h.returnMqtt()

	currentTemp := mqtt.NozzleTemp()
	waitForHeat := currentTemp == nil || *currentTemp < (tempC-filamentServiceHeatToleranceC)
	if waitForHeat {
		if sendErr := mqtt.SendGCode(r.Context(), fmt.Sprintf("M104 S%d", tempC)); sendErr != nil {
			h.writeError(w, http.StatusBadGateway, sendErr.Error())
			return
		}
		reached, err := waitForNozzle(mqtt, tempC)
		if err != nil {
			h.writeFilamentServiceError(w, err)
			return
		}
		v := reached
		currentTemp = &v
	}

	if err := mqtt.SendGCode(r.Context(), gcode); err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	var currentTempVal any
	if currentTemp != nil {
		currentTempVal = *currentTemp
	}
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"action":         action,
		"profile_id":     profile.ID,
		"profile_name":   profile.Name,
		"target_temp_c":  tempC,
		"current_temp_c": currentTempVal,
		"length_mm":      lengthMM,
		"gcode":          gcode,
	})
}

// FilamentServiceSwapStart begins a filament swap (unload).
func (h *Handler) FilamentServiceSwapStart(w http.ResponseWriter, r *http.Request) {
	payload := h.readJSONPayload(r)

	unloadProfile, err := h.filamentServiceProfile(payload, "unload_profile_id")
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	loadProfile, err := h.filamentServiceProfile(payload, "load_profile_id")
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	unloadTempC, err := filamentServiceTemp(unloadProfile)
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	loadTempC, err := filamentServiceTemp(loadProfile)
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	unloadLengthMM, err := filamentServiceLength(payload, "unload_length_mm")
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	loadLengthMM, err := filamentServiceLength(payload, "load_length_mm")
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}

	filamentSwapMu.Lock()
	if filamentSwapValue != nil {
		filamentSwapMu.Unlock()
		h.writeError(w, http.StatusConflict, "A filament swap is already in progress")
		return
	}
	filamentSwapMu.Unlock()

	gcode := buildFilamentMoveGcode(-unloadLengthMM)

	mqtt, err := h.borrowMqttForFilament()
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	defer h.returnMqtt()

	currentTemp := mqtt.NozzleTemp()
	if currentTemp == nil || *currentTemp < (unloadTempC-filamentServiceHeatToleranceC) {
		if sendErr := mqtt.SendGCode(r.Context(), fmt.Sprintf("M104 S%d", unloadTempC)); sendErr != nil {
			h.writeError(w, http.StatusBadGateway, sendErr.Error())
			return
		}
		if _, err := waitForNozzle(mqtt, unloadTempC); err != nil {
			h.writeFilamentServiceError(w, err)
			return
		}
	}
	if err := mqtt.SendGCode(r.Context(), gcode); err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	state := &filamentSwapState{
		Token:             generateToken(12),
		CreatedAt:         time.Now().Unix(),
		UnloadProfileID:   unloadProfile.ID,
		UnloadProfileName: unloadProfile.Name,
		LoadProfileID:     loadProfile.ID,
		LoadProfileName:   loadProfile.Name,
		UnloadTempC:       unloadTempC,
		LoadTempC:         loadTempC,
		UnloadLengthMM:    unloadLengthMM,
		LoadLengthMM:      loadLengthMM,
	}
	filamentSwapMu.Lock()
	filamentSwapValue = state
	filamentSwapMu.Unlock()

	resp := map[string]any{
		"status": "ok",
		"message": fmt.Sprintf("Unload started for %s. Swap the filament, then confirm to load %s.",
			unloadProfile.Name, loadProfile.Name),
		"gcode": gcode,
	}
	for k, v := range serializeFilamentSwapState(state) {
		resp[k] = v
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// FilamentServiceSwapConfirm completes a swap (load new filament).
func (h *Handler) FilamentServiceSwapConfirm(w http.ResponseWriter, r *http.Request) {
	payload := h.readJSONPayload(r)

	filamentSwapMu.Lock()
	state := filamentSwapValue
	if state == nil {
		filamentSwapMu.Unlock()
		h.writeError(w, http.StatusConflict, "No filament swap is in progress")
		return
	}
	if tok, ok := payload["token"].(string); ok && tok != "" && tok != state.Token {
		filamentSwapMu.Unlock()
		h.writeError(w, http.StatusConflict, "Swap token mismatch")
		return
	}
	filamentSwapMu.Unlock()

	gcode := buildFilamentMoveGcode(state.LoadLengthMM)

	mqtt, err := h.borrowMqttForFilament()
	if err != nil {
		h.writeFilamentServiceError(w, err)
		return
	}
	defer h.returnMqtt()

	currentTemp := mqtt.NozzleTemp()
	if currentTemp == nil || *currentTemp < (state.LoadTempC-filamentServiceHeatToleranceC) {
		if sendErr := mqtt.SendGCode(r.Context(), fmt.Sprintf("M104 S%d", state.LoadTempC)); sendErr != nil {
			h.writeError(w, http.StatusBadGateway, sendErr.Error())
			return
		}
		if _, err := waitForNozzle(mqtt, state.LoadTempC); err != nil {
			h.writeFilamentServiceError(w, err)
			return
		}
	}
	if err := mqtt.SendGCode(r.Context(), gcode); err != nil {
		h.writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	filamentSwapMu.Lock()
	filamentSwapValue = nil
	filamentSwapMu.Unlock()

	h.writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"message": fmt.Sprintf("Load / purge started for %s at %d\u00b0C.",
			state.LoadProfileName, state.LoadTempC),
		"gcode":          gcode,
		"completed_swap": state,
		"pending":        false,
		"swap":           nil,
	})
}

// FilamentServiceSwapCancel cancels a pending swap.
func (h *Handler) FilamentServiceSwapCancel(w http.ResponseWriter, r *http.Request) {
	payload := h.readJSONPayload(r)

	filamentSwapMu.Lock()
	state := filamentSwapValue
	if state == nil {
		filamentSwapMu.Unlock()
		h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "pending": false, "swap": nil})
		return
	}
	if tok, ok := payload["token"].(string); ok && tok != "" && tok != state.Token {
		filamentSwapMu.Unlock()
		h.writeError(w, http.StatusConflict, "Swap token mismatch")
		return
	}
	filamentSwapValue = nil
	filamentSwapMu.Unlock()

	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"message":         "Filament swap cancelled.",
		"cancelled_swap":  state,
		"pending":         false,
		"swap":            nil,
	})
}

// readJSONPayload is a convenience to parse a JSON body. Returns empty map on failure.
func (h *Handler) readJSONPayload(r *http.Request) map[string]any {
	var payload map[string]any
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		_ = dec.Decode(&payload)
	}
	if payload == nil {
		payload = make(map[string]any)
	}
	return payload
}

