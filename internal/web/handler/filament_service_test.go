package handler

import (
	"testing"
)

func TestFormatExtrusionMM(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{40.0, "40"},
		{40.50, "40.5"},
		{40.12, "40.12"},
		{-40.0, "-40"},
		{-40.50, "-40.5"},
		{0.0, "0"},
		{0.10, "0.1"},
		{1.00, "1"},
	}
	for _, tt := range tests {
		got := formatExtrusionMM(tt.input)
		if got != tt.want {
			t.Errorf("formatExtrusionMM(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildFilamentMoveGcode(t *testing.T) {
	gcode := buildFilamentMoveGcode(40.0)
	if gcode == "" {
		t.Fatal("expected non-empty gcode")
	}
	// Must contain M83 (relative extrusion), G1 extrude, M400 (wait), M82 (absolute).
	if !contains(gcode, "M83") || !contains(gcode, "M82") || !contains(gcode, "M400") {
		t.Errorf("gcode missing expected commands: %s", gcode)
	}
	if !contains(gcode, "G1 E40 F240") {
		t.Errorf("expected G1 E40 F240, got: %s", gcode)
	}
	// Must NOT contain G92 E0 (extruder reset removed per Python update).
	if contains(gcode, "G92") {
		t.Errorf("gcode must not contain G92, got: %s", gcode)
	}
	// Order check: M83 before G1, M400 before M82.
	wantLines := []string{"M83", "G1 E40 F240", "M400", "M82"}
	lines := splitLines(gcode)
	for i, want := range wantLines {
		if i >= len(lines) {
			t.Fatalf("gcode has only %d lines, want at least %d; got:\n%s", len(lines), len(wantLines), gcode)
		}
		if lines[i] != want {
			t.Errorf("line[%d] = %q, want %q", i, lines[i], want)
		}
	}
	if len(lines) != len(wantLines) {
		t.Errorf("gcode has %d lines, want %d:\n%s", len(lines), len(wantLines), gcode)
	}

	// Retract (negative).
	retract := buildFilamentMoveGcode(-25.5)
	if !contains(retract, "G1 E-25.5 F240") {
		t.Errorf("expected G1 E-25.5 F240, got: %s", retract)
	}
	if contains(retract, "G92") {
		t.Errorf("retract gcode must not contain G92, got: %s", retract)
	}
}

func TestBuildFilamentMoveGcode_Feedrate(t *testing.T) {
	// Feedrate must always be 240 regardless of input.
	for _, mm := range []float64{1.0, 40.0, 100.0, -40.0} {
		gcode := buildFilamentMoveGcode(mm)
		if !contains(gcode, "F240") {
			t.Errorf("buildFilamentMoveGcode(%v) missing F240: %s", mm, gcode)
		}
	}
}

func TestSerializeFilamentSwapState_NewFields(t *testing.T) {
	// Ensure new fields are included in serialization.
	state := &filamentSwapState{
		Token:                  "tok",
		CreatedAt:              1000,
		Mode:                   "manual",
		Phase:                  phaseAwaitManual,
		Message:                "test message",
		ManualSwapPreheatTempC: 140,
	}
	result := serializeFilamentSwapState(state)
	if result["pending"] != true {
		t.Errorf("pending = %v, want true", result["pending"])
	}
	swap, ok := result["swap"].(map[string]any)
	if !ok {
		t.Fatal("swap is not a map")
	}
	if swap["mode"] != "manual" {
		t.Errorf("mode = %v, want manual", swap["mode"])
	}
	if swap["phase"] != phaseAwaitManual {
		t.Errorf("phase = %v, want %s", swap["phase"], phaseAwaitManual)
	}
	if swap["manual_swap_preheat_temp_c"] != 140 {
		t.Errorf("manual_swap_preheat_temp_c = %v, want 140", swap["manual_swap_preheat_temp_c"])
	}
	// message non-empty → must not be nil
	if swap["message"] == nil {
		t.Errorf("message = nil, want non-nil for non-empty message")
	}
	// error empty → must be nil
	if swap["error"] != nil {
		t.Errorf("error = %v, want nil for empty error", swap["error"])
	}
}

func TestFilamentServiceLength(t *testing.T) {
	// Default when key is missing.
	payload := map[string]any{}
	l, err := filamentServiceLength(payload, "length_mm")
	if err != nil || l != filamentServiceDefaultLengthMM {
		t.Errorf("default: got %v, %v", l, err)
	}

	// Valid value.
	payload = map[string]any{"length_mm": 100.0}
	l, err = filamentServiceLength(payload, "length_mm")
	if err != nil || l != 100.0 {
		t.Errorf("valid: got %v, %v", l, err)
	}

	// Over max.
	payload = map[string]any{"length_mm": 999.0}
	_, err = filamentServiceLength(payload, "length_mm")
	if err == nil {
		t.Error("expected error for over-max length")
	}

	// Zero.
	payload = map[string]any{"length_mm": 0.0}
	_, err = filamentServiceLength(payload, "length_mm")
	if err == nil {
		t.Error("expected error for zero length")
	}

	// Negative.
	payload = map[string]any{"length_mm": -5.0}
	_, err = filamentServiceLength(payload, "length_mm")
	if err == nil {
		t.Error("expected error for negative length")
	}
}

func TestSerializeFilamentSwapState(t *testing.T) {
	// nil state.
	result := serializeFilamentSwapState(nil)
	if result["pending"] != false {
		t.Errorf("nil state: pending = %v, want false", result["pending"])
	}
	if result["swap"] != nil {
		t.Errorf("nil state: swap = %v, want nil", result["swap"])
	}

	// non-nil state with pointer profile fields.
	unloadID := int64(1)
	unloadName := "PLA"
	loadID := int64(2)
	loadName := "PETG"
	state := &filamentSwapState{
		Token:                  "abc123",
		CreatedAt:              1000,
		Mode:                   "legacy",
		Phase:                  phaseAwaitManual,
		UnloadProfileID:        &unloadID,
		UnloadProfileName:      &unloadName,
		LoadProfileID:          &loadID,
		LoadProfileName:        &loadName,
		UnloadTempC:            210,
		LoadTempC:              230,
		UnloadLengthMM:         40,
		LoadLengthMM:           50,
		ManualSwapPreheatTempC: 140,
	}
	result = serializeFilamentSwapState(state)
	if result["pending"] != true {
		t.Errorf("pending = %v, want true", result["pending"])
	}
	swap, ok := result["swap"].(map[string]any)
	if !ok {
		t.Fatal("swap is not a map")
	}
	if swap["token"] != "abc123" {
		t.Errorf("token = %v, want abc123", swap["token"])
	}
	if swap["mode"] != "legacy" {
		t.Errorf("mode = %v, want legacy", swap["mode"])
	}
	if swap["unload_profile_id"] != &unloadID {
		t.Errorf("unload_profile_id = %v, want %v", swap["unload_profile_id"], &unloadID)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
