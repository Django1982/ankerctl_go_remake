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
	// Check it contains M83 (relative extrusion) and M82 (absolute).
	if !contains(gcode, "M83") || !contains(gcode, "M82") || !contains(gcode, "M400") {
		t.Errorf("gcode missing expected commands: %s", gcode)
	}
	if !contains(gcode, "G1 E40 F240") {
		t.Errorf("expected G1 E40 F240, got: %s", gcode)
	}

	// Retract (negative).
	retract := buildFilamentMoveGcode(-25.5)
	if !contains(retract, "G1 E-25.5 F240") {
		t.Errorf("expected G1 E-25.5 F240, got: %s", retract)
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

	// non-nil state.
	state := &filamentSwapState{
		Token:             "abc123",
		CreatedAt:         1000,
		UnloadProfileID:   1,
		UnloadProfileName: "PLA",
		LoadProfileID:     2,
		LoadProfileName:   "PETG",
		UnloadTempC:       210,
		LoadTempC:         230,
		UnloadLengthMM:    40,
		LoadLengthMM:      50,
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
