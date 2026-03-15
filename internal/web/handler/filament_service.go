package handler

import (
	"context"
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
	"github.com/django1982/ankerctl/internal/model"
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

// Swap phase constants — used in phase-guard checks.
const (
	phaseHeatingUnload  = "heating_unload"
	phaseUnloading      = "unloading"
	phaseHeatingLoad    = "heating_load"
	phaseLoading        = "loading"
	phaseAwaitManual    = "await_manual_swap"
	phaseError          = "error"
)

// activePhases is the set of phases during which confirm/cancel are blocked (409).
var activePhases = map[string]bool{
	phaseHeatingUnload: true,
	phaseUnloading:     true,
	phaseHeatingLoad:   true,
	phaseLoading:       true,
}

// filamentSwapState holds the state of an in-progress filament swap.
type filamentSwapState struct {
	Token                  string  `json:"token"`
	CreatedAt              int64   `json:"created_at"`
	Mode                   string  `json:"mode"`    // "manual" | "legacy"
	Phase                  string  `json:"phase"`   // see phase constants above
	Message                string  `json:"message"`
	Error                  string  `json:"error"`
	UnloadProfileID        *int64  `json:"unload_profile_id"`
	UnloadProfileName      *string `json:"unload_profile_name"`
	LoadProfileID          *int64  `json:"load_profile_id"`
	LoadProfileName        *string `json:"load_profile_name"`
	UnloadTempC            int     `json:"unload_temp_c"`
	LoadTempC              int     `json:"load_temp_c"`
	UnloadLengthMM         float64 `json:"unload_length_mm"`
	LoadLengthMM           float64 `json:"load_length_mm"`
	ManualSwapPreheatTempC int     `json:"manual_swap_preheat_temp_c"`
}

// filamentSwapManager holds the mutex-protected swap state.
// It lives as package-level vars so it's shared across requests.
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
// Feedrate is always 240 mm/min. Order: M83, G1, M400, M82.
// No G92 E0 lines — the printer's relative mode is used directly.
func buildFilamentMoveGcode(deltaMM float64) string {
	extrusion := formatExtrusionMM(deltaMM)
	return strings.Join([]string{
		"M83",
		fmt.Sprintf("G1 E%s F%d", extrusion, filamentServiceFeedrateMMMin),
		"M400",
		"M82",
	}, "\n")
}

// serializeFilamentSwapState produces the JSON-compatible map for a swap state.
func serializeFilamentSwapState(state *filamentSwapState) map[string]any {
	if state == nil {
		return map[string]any{"pending": false, "swap": nil}
	}

	swap := map[string]any{
		"token":                    state.Token,
		"created_at":               state.CreatedAt,
		"mode":                     state.Mode,
		"phase":                    state.Phase,
		"message":                  nilIfEmpty(state.Message),
		"error":                    nilIfEmpty(state.Error),
		"unload_profile_id":        state.UnloadProfileID,
		"unload_profile_name":      state.UnloadProfileName,
		"load_profile_id":          state.LoadProfileID,
		"load_profile_name":        state.LoadProfileName,
		"unload_temp_c":            state.UnloadTempC,
		"load_temp_c":              state.LoadTempC,
		"unload_length_mm":         state.UnloadLengthMM,
		"load_length_mm":           state.LoadLengthMM,
		"manual_swap_preheat_temp_c": state.ManualSwapPreheatTempC,
	}
	return map[string]any{
		"pending": true,
		"swap":    swap,
	}
}

// nilIfEmpty returns nil for empty strings so JSON serializes as null.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
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

// loadFilamentServiceConfig reads the filament service config from disk,
// returning defaults when no config is stored yet.
func (h *Handler) loadFilamentServiceConfig() model.FilamentServiceConfig {
	cfg, err := h.loadConfig()
	if err != nil || cfg == nil {
		return model.DefaultFilamentServiceConfig()
	}
	fs := cfg.FilamentService
	// Always clamp in case of stale on-disk data.
	fs.ManualSwapPreheatTempC = model.ClampManualSwapPreheatTempC(fs.ManualSwapPreheatTempC)
	return fs
}

// swapStateUpdate applies updates to the global swap state under the mutex.
// A no-op when no state exists or the token doesn't match.
func swapStateUpdate(token string, fn func(s *filamentSwapState)) {
	filamentSwapMu.Lock()
	defer filamentSwapMu.Unlock()
	if filamentSwapValue == nil || filamentSwapValue.Token != token {
		return
	}
	fn(filamentSwapValue)
}

// swapStateClear removes the global swap state if the token matches.
// Returns a copy of the cleared state, or nil if nothing was cleared.
func swapStateClear(token string) *filamentSwapState {
	filamentSwapMu.Lock()
	defer filamentSwapMu.Unlock()
	if filamentSwapValue == nil || filamentSwapValue.Token != token {
		return nil
	}
	copy := *filamentSwapValue
	filamentSwapValue = nil
	return &copy
}

// swapStateGet returns a copy of the current state if the token matches.
func swapStateGet(token string) *filamentSwapState {
	filamentSwapMu.Lock()
	defer filamentSwapMu.Unlock()
	if filamentSwapValue == nil || filamentSwapValue.Token != token {
		return nil
	}
	copy := *filamentSwapValue
	return &copy
}

// runLegacySwapUnload runs in a goroutine: heats + unloads, then waits for confirm.
func (h *Handler) runLegacySwapUnload(token string) {
	state := swapStateGet(token)
	if state == nil {
		return
	}

	gcode := buildFilamentMoveGcode(-state.UnloadLengthMM)

	svc, borrowErr := h.svc.Borrow("mqttqueue")
	if borrowErr != nil {
		swapStateUpdate(token, func(s *filamentSwapState) {
			errMsg := "MQTT service unavailable"
			s.Phase = phaseError
			s.Message = "Automatic unload failed: " + errMsg
			s.Error = errMsg
		})
		return
	}
	mqtt, ok := svc.(*service.MqttQueue)
	if !ok {
		h.svc.Return("mqttqueue")
		swapStateUpdate(token, func(s *filamentSwapState) {
			errMsg := "MQTT service type mismatch"
			s.Phase = phaseError
			s.Message = "Automatic unload failed: " + errMsg
			s.Error = errMsg
		})
		return
	}
	defer h.svc.Return("mqttqueue")

	if err := assertFilamentServiceReady(mqtt); err != nil {
		swapStateUpdate(token, func(s *filamentSwapState) {
			s.Phase = phaseError
			s.Message = "Automatic unload failed: " + err.Error()
			s.Error = err.Error()
		})
		return
	}

	currentTemp := mqtt.NozzleTemp()
	if currentTemp == nil || *currentTemp < (state.UnloadTempC-filamentServiceHeatToleranceC) {
		unloadTempC := state.UnloadTempC
		swapStateUpdate(token, func(s *filamentSwapState) {
			s.Phase = phaseHeatingUnload
			s.Message = fmt.Sprintf("Heating nozzle to %d\u00b0C for unload...", unloadTempC)
			s.Error = ""
		})
		if err := mqtt.SendGCode(context.Background(), fmt.Sprintf("M104 S%d", state.UnloadTempC)); err != nil {
			swapStateUpdate(token, func(s *filamentSwapState) {
				s.Phase = phaseError
				s.Message = "Automatic unload failed: " + err.Error()
				s.Error = err.Error()
			})
			return
		}
		if _, err := waitForNozzle(mqtt, state.UnloadTempC); err != nil {
			swapStateUpdate(token, func(s *filamentSwapState) {
				s.Phase = phaseError
				s.Message = "Automatic unload failed: " + err.Error()
				s.Error = err.Error()
			})
			return
		}
	}

	profileName := ""
	if state.UnloadProfileName != nil {
		profileName = *state.UnloadProfileName
	}
	unloadLengthMM := state.UnloadLengthMM
	swapStateUpdate(token, func(s *filamentSwapState) {
		s.Phase = phaseUnloading
		s.Message = fmt.Sprintf("Retracting %.2g mm for %s...", unloadLengthMM, profileName)
		s.Error = ""
	})
	if err := mqtt.SendGCode(context.Background(), gcode); err != nil {
		swapStateUpdate(token, func(s *filamentSwapState) {
			s.Phase = phaseError
			s.Message = "Automatic unload failed: " + err.Error()
			s.Error = err.Error()
		})
		return
	}

	swapStateUpdate(token, func(s *filamentSwapState) {
		s.Phase = phaseAwaitManual
		s.Message = "Unload finished. Release the extruder lever, remove the old filament, " +
			"insert the new filament, then confirm."
		s.Error = ""
	})
}

// runLegacySwapLoad runs in a goroutine: heats + loads/purges, then clears state.
func (h *Handler) runLegacySwapLoad(token string) {
	state := swapStateGet(token)
	if state == nil {
		return
	}

	gcode := buildFilamentMoveGcode(state.LoadLengthMM)

	svc, borrowErr := h.svc.Borrow("mqttqueue")
	if borrowErr != nil {
		swapStateUpdate(token, func(s *filamentSwapState) {
			errMsg := "MQTT service unavailable"
			s.Phase = phaseError
			s.Message = "Automatic load / purge failed: " + errMsg
			s.Error = errMsg
		})
		return
	}
	mqtt, ok := svc.(*service.MqttQueue)
	if !ok {
		h.svc.Return("mqttqueue")
		swapStateUpdate(token, func(s *filamentSwapState) {
			errMsg := "MQTT service type mismatch"
			s.Phase = phaseError
			s.Message = "Automatic load / purge failed: " + errMsg
			s.Error = errMsg
		})
		return
	}
	defer h.svc.Return("mqttqueue")

	if err := assertFilamentServiceReady(mqtt); err != nil {
		swapStateUpdate(token, func(s *filamentSwapState) {
			s.Phase = phaseError
			s.Message = "Automatic load / purge failed: " + err.Error()
			s.Error = err.Error()
		})
		return
	}

	currentTemp := mqtt.NozzleTemp()
	if currentTemp == nil || *currentTemp < (state.LoadTempC-filamentServiceHeatToleranceC) {
		loadTempC := state.LoadTempC
		swapStateUpdate(token, func(s *filamentSwapState) {
			s.Phase = phaseHeatingLoad
			s.Message = fmt.Sprintf("Heating nozzle to %d\u00b0C for load / purge...", loadTempC)
			s.Error = ""
		})
		if err := mqtt.SendGCode(context.Background(), fmt.Sprintf("M104 S%d", state.LoadTempC)); err != nil {
			swapStateUpdate(token, func(s *filamentSwapState) {
				s.Phase = phaseError
				s.Message = "Automatic load / purge failed: " + err.Error()
				s.Error = err.Error()
			})
			return
		}
		if _, err := waitForNozzle(mqtt, state.LoadTempC); err != nil {
			swapStateUpdate(token, func(s *filamentSwapState) {
				s.Phase = phaseError
				s.Message = "Automatic load / purge failed: " + err.Error()
				s.Error = err.Error()
			})
			return
		}
	}

	profileName := ""
	if state.LoadProfileName != nil {
		profileName = *state.LoadProfileName
	}
	loadLengthMM := state.LoadLengthMM
	swapStateUpdate(token, func(s *filamentSwapState) {
		s.Phase = phaseLoading
		s.Message = fmt.Sprintf("Loading / purging %s (%.2g mm)...", profileName, loadLengthMM)
		s.Error = ""
	})
	if err := mqtt.SendGCode(context.Background(), gcode); err != nil {
		swapStateUpdate(token, func(s *filamentSwapState) {
			s.Phase = phaseError
			s.Message = "Automatic load / purge failed: " + err.Error()
			s.Error = err.Error()
		})
		return
	}

	// Success: clear state.
	swapStateClear(token)
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
		"status":        "ok",
		"action":        "preheat",
		"profile_id":    profile.ID,
		"profile_name":  profile.Name,
		"target_temp_c": tempC,
		"gcode":         gcode,
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

// FilamentServiceSwapStart begins a filament swap.
//
// In manual mode (allow_legacy_swap=false, default): sends M104 S{preheat_temp},
// stores state with mode="manual", phase="await_manual_swap", returns immediately.
//
// In legacy mode (allow_legacy_swap=true): requires unload_profile_id, load_profile_id,
// unload_length_mm, load_length_mm. Starts a background goroutine for unload.
func (h *Handler) FilamentServiceSwapStart(w http.ResponseWriter, r *http.Request) {
	payload := h.readJSONPayload(r)

	fsCfg := h.loadFilamentServiceConfig()
	allowLegacy := fsCfg.AllowLegacySwap
	preheatTempC := fsCfg.ManualSwapPreheatTempC

	// Profile/length fields are only required in legacy mode.
	var (
		unloadProfile *db.FilamentProfile
		loadProfile   *db.FilamentProfile
		unloadTempC   = preheatTempC
		loadTempC     = preheatTempC
		unloadLengthMM float64
		loadLengthMM   float64
	)

	if allowLegacy {
		var err error
		unloadProfile, err = h.filamentServiceProfile(payload, "unload_profile_id")
		if err != nil {
			h.writeFilamentServiceError(w, err)
			return
		}
		loadProfile, err = h.filamentServiceProfile(payload, "load_profile_id")
		if err != nil {
			h.writeFilamentServiceError(w, err)
			return
		}
		unloadTempC, err = filamentServiceTemp(unloadProfile)
		if err != nil {
			h.writeFilamentServiceError(w, err)
			return
		}
		loadTempC, err = filamentServiceTemp(loadProfile)
		if err != nil {
			h.writeFilamentServiceError(w, err)
			return
		}
		unloadLengthMM, err = filamentServiceLength(payload, "unload_length_mm")
		if err != nil {
			h.writeFilamentServiceError(w, err)
			return
		}
		loadLengthMM, err = filamentServiceLength(payload, "load_length_mm")
		if err != nil {
			h.writeFilamentServiceError(w, err)
			return
		}
	}

	// Conflict check — only one swap in progress at a time.
	filamentSwapMu.Lock()
	if filamentSwapValue != nil {
		filamentSwapMu.Unlock()
		h.writeError(w, http.StatusConflict, "A filament swap is already in progress")
		return
	}
	filamentSwapMu.Unlock()

	mode := "manual"
	phase := phaseAwaitManual
	message := fmt.Sprintf(
		"Recommended method enabled: preheating nozzle to %d\u00b0C. "+
			"Release the extruder lever, remove the filament manually, insert the new filament, "+
			"then confirm. Use Quick Extrude afterward if you need to purge.", preheatTempC)

	if allowLegacy {
		mode = "legacy"
		phase = phaseHeatingUnload
		profileName := ""
		if unloadProfile != nil {
			profileName = unloadProfile.Name
		}
		message = fmt.Sprintf("Heating for automatic unload of %s at %d\u00b0C.",
			profileName, unloadTempC)
	}

	state := &filamentSwapState{
		Token:                  generateToken(12),
		CreatedAt:              time.Now().Unix(),
		Mode:                   mode,
		Phase:                  phase,
		Message:                message,
		ManualSwapPreheatTempC: preheatTempC,
		UnloadTempC:            unloadTempC,
		LoadTempC:              loadTempC,
		UnloadLengthMM:         unloadLengthMM,
		LoadLengthMM:           loadLengthMM,
	}
	if unloadProfile != nil {
		id := unloadProfile.ID
		name := unloadProfile.Name
		state.UnloadProfileID = &id
		state.UnloadProfileName = &name
	}
	if loadProfile != nil {
		id := loadProfile.ID
		name := loadProfile.Name
		state.LoadProfileID = &id
		state.LoadProfileName = &name
	}

	// Register state before sending GCode so the background goroutine can find it.
	filamentSwapMu.Lock()
	filamentSwapValue = state
	filamentSwapMu.Unlock()

	// Borrow MQTT, assert ready, then kick off the appropriate action.
	svc, borrowErr := h.svc.Borrow("mqttqueue")
	if borrowErr != nil {
		swapStateClear(state.Token)
		h.writeFilamentServiceError(w, &connectionError{"MQTT service unavailable"})
		return
	}
	mqtt, ok := svc.(*service.MqttQueue)
	if !ok {
		h.svc.Return("mqttqueue")
		swapStateClear(state.Token)
		h.writeFilamentServiceError(w, &connectionError{"MQTT service type mismatch"})
		return
	}

	if err := assertFilamentServiceReady(mqtt); err != nil {
		h.svc.Return("mqttqueue")
		swapStateClear(state.Token)
		h.writeFilamentServiceError(w, err)
		return
	}

	var gcodeResp any
	if allowLegacy {
		// Start background goroutine; release borrow first so the goroutine can re-borrow.
		h.svc.Return("mqttqueue")
		go h.runLegacySwapUnload(state.Token)
	} else {
		// Manual mode: just send preheat and return.
		sendErr := mqtt.SendGCode(r.Context(), fmt.Sprintf("M104 S%d", preheatTempC))
		h.svc.Return("mqttqueue")
		if sendErr != nil {
			swapStateClear(state.Token)
			h.writeError(w, http.StatusBadGateway, sendErr.Error())
			return
		}
		gcodeResp = fmt.Sprintf("M104 S%d", preheatTempC)
	}

	filamentSwapMu.Lock()
	currentState := filamentSwapValue
	filamentSwapMu.Unlock()

	resp := map[string]any{
		"status":  "ok",
		"message": state.Message,
		"gcode":   gcodeResp,
	}
	for k, v := range serializeFilamentSwapState(currentState) {
		resp[k] = v
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// FilamentServiceSwapConfirm completes a swap (load new filament).
//
// Manual mode: clears state immediately, returns pending=false.
// Legacy mode: launches background load goroutine, returns current state.
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
	if activePhases[state.Phase] {
		filamentSwapMu.Unlock()
		h.writeError(w, http.StatusConflict, "Swap stage is still running; wait for it to finish first")
		return
	}
	stateCopy := *state
	filamentSwapMu.Unlock()

	// Manual mode: just clear and return done.
	if stateCopy.Mode == "manual" {
		swapStateClear(stateCopy.Token)
		h.writeJSON(w, http.StatusOK, map[string]any{
			"status": "ok",
			"message": "Manual swap marked complete. If needed, use Quick Extrude to prime " +
				"the new filament.",
			"completed_swap": stateCopy,
			"pending":        false,
			"swap":           nil,
		})
		return
	}

	// Legacy mode: transition to heating_load and start background load.
	profileName := ""
	if stateCopy.LoadProfileName != nil {
		profileName = *stateCopy.LoadProfileName
	}
	swapStateUpdate(stateCopy.Token, func(s *filamentSwapState) {
		s.Phase = phaseHeatingLoad
		s.Message = fmt.Sprintf("Heating for automatic load / purge of %s at %d\u00b0C.",
			profileName, stateCopy.LoadTempC)
		s.Error = ""
	})

	// Verify MQTT is accessible before launching goroutine.
	svc, borrowErr := h.svc.Borrow("mqttqueue")
	if borrowErr != nil {
		swapStateUpdate(stateCopy.Token, func(s *filamentSwapState) {
			errMsg := "MQTT service unavailable"
			s.Phase = phaseError
			s.Message = errMsg
			s.Error = errMsg
		})
		h.writeFilamentServiceError(w, &connectionError{"MQTT service unavailable"})
		return
	}
	mqtt, ok := svc.(*service.MqttQueue)
	if !ok {
		h.svc.Return("mqttqueue")
		swapStateUpdate(stateCopy.Token, func(s *filamentSwapState) {
			errMsg := "MQTT service type mismatch"
			s.Phase = phaseError
			s.Message = errMsg
			s.Error = errMsg
		})
		h.writeFilamentServiceError(w, &connectionError{"MQTT service type mismatch"})
		return
	}
	if err := assertFilamentServiceReady(mqtt); err != nil {
		h.svc.Return("mqttqueue")
		swapStateUpdate(stateCopy.Token, func(s *filamentSwapState) {
			s.Phase = phaseError
			s.Message = err.Error()
			s.Error = err.Error()
		})
		h.writeFilamentServiceError(w, err)
		return
	}
	h.svc.Return("mqttqueue")

	go h.runLegacySwapLoad(stateCopy.Token)

	currentState := swapStateGet(stateCopy.Token)
	resp := map[string]any{
		"status": "ok",
	}
	if currentState != nil {
		resp["message"] = currentState.Message
	}
	for k, v := range serializeFilamentSwapState(currentState) {
		resp[k] = v
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// FilamentServiceSwapCancel cancels a pending swap.
// Returns 409 if a swap stage is actively running (phase guard).
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
	if activePhases[state.Phase] {
		filamentSwapMu.Unlock()
		h.writeError(w, http.StatusConflict, "Cannot cancel while an automatic swap stage is running")
		return
	}
	stateCopy := *state
	filamentSwapValue = nil
	filamentSwapMu.Unlock()

	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"message":        "Filament swap cancelled.",
		"cancelled_swap": stateCopy,
		"pending":        false,
		"swap":           nil,
	})
}

// SettingsFilamentServiceGet returns the filament service settings.
// GET /api/settings/filament-service — auth-protected.
func (h *Handler) SettingsFilamentServiceGet(w http.ResponseWriter, _ *http.Request) {
	cfg, err := h.loadConfig()
	if err != nil || cfg == nil {
		h.writeError(w, http.StatusBadRequest, "No printers configured")
		return
	}
	fs := cfg.FilamentService
	fs.ManualSwapPreheatTempC = model.ClampManualSwapPreheatTempC(fs.ManualSwapPreheatTempC)
	h.writeJSON(w, http.StatusOK, map[string]any{"filament_service": fs})
}

// SettingsFilamentServiceUpdate saves filament service settings.
// POST /api/settings/filament-service — auth-protected.
//
// Accepts both:
//   {"filament_service": {"allow_legacy_swap": false, "manual_swap_preheat_temp_c": 140}}
// and flat form:
//   {"allow_legacy_swap": false, "manual_swap_preheat_temp_c": 140}
func (h *Handler) SettingsFilamentServiceUpdate(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		h.writeError(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	// Allow either wrapped or flat payload (Python parity).
	fsPayload := raw
	if inner, ok := raw["filament_service"]; ok {
		m, ok := inner.(map[string]any)
		if !ok {
			h.writeError(w, http.StatusBadRequest, "Invalid filament_service payload")
			return
		}
		fsPayload = m
	}

	var updated model.FilamentServiceConfig
	if err := h.cfg.Modify(func(cfg *model.Config) (*model.Config, error) {
		if cfg == nil {
			return cfg, nil
		}
		updated = cfg.FilamentService
		// Apply known fields explicitly for type safety.
		if v, ok := fsPayload["allow_legacy_swap"]; ok {
			switch b := v.(type) {
			case bool:
				updated.AllowLegacySwap = b
			case float64:
				updated.AllowLegacySwap = b != 0
			}
		}
		if v, ok := fsPayload["manual_swap_preheat_temp_c"]; ok {
			switch n := v.(type) {
			case float64:
				updated.ManualSwapPreheatTempC = int(n)
			case json.Number:
				if i, err := n.Int64(); err == nil {
					updated.ManualSwapPreheatTempC = int(i)
				} else {
					return nil, &badRequestError{"manual_swap_preheat_temp_c must be an integer"}
				}
			default:
				return nil, &badRequestError{"manual_swap_preheat_temp_c must be an integer"}
			}
		}
		// Always clamp after merging.
		updated.ManualSwapPreheatTempC = model.ClampManualSwapPreheatTempC(updated.ManualSwapPreheatTempC)
		cfg.FilamentService = updated
		return cfg, nil
	}); err != nil {
		var br *badRequestError
		if isBadRequest(err, &br) {
			h.writeError(w, http.StatusBadRequest, br.msg)
			return
		}
		h.writeError(w, http.StatusInternalServerError, "failed to update filament service settings")
		return
	}

	h.writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "filament_service": updated})
}

// badRequestError is a sentinel for validation errors inside cfg.Modify closures.
type badRequestError struct{ msg string }

func (e *badRequestError) Error() string { return e.msg }

func isBadRequest(err error, out **badRequestError) bool {
	if br, ok := err.(*badRequestError); ok {
		*out = br
		return true
	}
	return false
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
