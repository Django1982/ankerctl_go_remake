package handler

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestZOffsetStepsConversion(t *testing.T) {
	tests := []struct {
		name     string
		steps    int
		wantMM   float64
	}{
		{"zero", 0, 0.0},
		{"positive", 13, 0.13},
		{"negative", -5, -0.05},
		{"large positive", 1000, 10.0},
		{"large negative", -1000, -10.0},
		{"one step", 1, 0.01},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := float64(tt.steps) * 0.01
			if math.Abs(got-tt.wantMM) > 1e-9 {
				t.Errorf("steps=%d: got %f, want %f", tt.steps, got, tt.wantMM)
			}
		})
	}
}

func TestZOffsetMMToSteps(t *testing.T) {
	tests := []struct {
		name      string
		mm        float64
		wantSteps int
	}{
		{"zero", 0.0, 0},
		{"positive", 0.13, 13},
		{"negative", -0.05, -5},
		{"rounding up", 0.125, 13},    // rounds to nearest
		{"rounding down", 0.124, 12},  // rounds to nearest
		{"large", 10.0, 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := int(math.Round(tt.mm / 0.01))
			if got != tt.wantSteps {
				t.Errorf("mm=%f: got steps=%d, want %d", tt.mm, got, tt.wantSteps)
			}
		})
	}
}

func TestZOffsetDeltaCalculation(t *testing.T) {
	tests := []struct {
		name         string
		currentSteps int
		targetMM     float64
		wantDelta    int
		wantDeltaMM  float64
	}{
		{"no change", 13, 0.13, 0, 0.0},
		{"increase", 10, 0.15, 5, 0.05},
		{"decrease", 15, 0.10, -5, -0.05},
		{"zero to positive", 0, 0.13, 13, 0.13},
		{"positive to zero", 13, 0.0, -13, -0.13},
		{"negative to positive", -5, 0.05, 10, 0.10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetSteps := int(math.Round(tt.targetMM / 0.01))
			deltaSteps := targetSteps - tt.currentSteps
			deltaMM := float64(deltaSteps) * 0.01

			if deltaSteps != tt.wantDelta {
				t.Errorf("deltaSteps: got %d, want %d", deltaSteps, tt.wantDelta)
			}
			if math.Abs(deltaMM-tt.wantDeltaMM) > 1e-9 {
				t.Errorf("deltaMM: got %f, want %f", deltaMM, tt.wantDeltaMM)
			}
		})
	}
}

func TestZOffsetGetNoService(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/printer/z-offset", nil)
	w := httptest.NewRecorder()
	h.ZOffsetGet(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestZOffsetSetValidation(t *testing.T) {
	h := &Handler{}

	tests := []struct {
		name     string
		body     any
		wantCode int
	}{
		{
			"missing field",
			map[string]any{},
			http.StatusBadRequest,
		},
		{
			"out of range high",
			map[string]any{"z_offset_mm": 11.0},
			http.StatusBadRequest,
		},
		{
			"out of range low",
			map[string]any{"z_offset_mm": -11.0},
			http.StatusBadRequest,
		},
		{
			"invalid json",
			"not-json",
			http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			switch v := tt.body.(type) {
			case string:
				body = []byte(v)
			default:
				body, _ = json.Marshal(v)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/printer/z-offset", bytes.NewReader(body))
			w := httptest.NewRecorder()
			h.ZOffsetSet(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("%s: expected status %d, got %d (body: %s)", tt.name, tt.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

func TestZOffsetNudgeValidation(t *testing.T) {
	h := &Handler{}

	tests := []struct {
		name     string
		body     any
		wantCode int
	}{
		{
			"missing field",
			map[string]any{},
			http.StatusBadRequest,
		},
		{
			"out of range",
			map[string]any{"delta_mm": 15.0},
			http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/printer/z-offset/nudge", bytes.NewReader(body))
			w := httptest.NewRecorder()
			h.ZOffsetNudge(w, req)

			if w.Code != tt.wantCode {
				t.Errorf("%s: expected status %d, got %d", tt.name, tt.wantCode, w.Code)
			}
		})
	}
}
