package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/mqtt/protocol"
)

func intPtr(v int) *int {
	return &v
}

func TestZAxisRecoupParsing(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:   make(chan struct{}, 1),
		bedLevelingGrid: make(map[string]any),
	}

	tests := []struct {
		name    string
		payload map[string]any
		want    *int
	}{
		{"value 13", map[string]any{"commandType": int(protocol.MqttCmdZAxisRecoup), "value": 13}, intPtr(13)},
		{"value 0", map[string]any{"commandType": int(protocol.MqttCmdZAxisRecoup), "value": 0}, intPtr(0)},
		{"value -5", map[string]any{"commandType": int(protocol.MqttCmdZAxisRecoup), "value": -5}, intPtr(-5)},
		{"no value key", map[string]any{"commandType": int(protocol.MqttCmdZAxisRecoup)}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q.mu.Lock()
			q.zAxisRecoup = nil
			q.mu.Unlock()
			// Drain channel
			select {
			case <-q.zAxisRecoupCh:
			default:
			}

			q.handlePayload(tt.payload)

			got := q.ZAxisRecoup()
			if tt.want == nil {
				if got != nil {
					t.Errorf("expected nil, got %d", *got)
				}
			} else {
				if got == nil {
					t.Errorf("expected %d, got nil", *tt.want)
				} else if *got != *tt.want {
					t.Errorf("expected %d, got %d", *tt.want, *got)
				}
			}
		})
	}
}

func TestZOffsetMM(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:   make(chan struct{}, 1),
		bedLevelingGrid: make(map[string]any),
	}

	// No data yet
	if _, ok := q.ZOffsetMM(); ok {
		t.Error("expected unknown z-offset before any ct=1021")
	}

	// Set value
	v := 13
	q.mu.Lock()
	q.zAxisRecoup = &v
	q.mu.Unlock()

	mm, ok := q.ZOffsetMM()
	if !ok {
		t.Fatal("expected known z-offset")
	}
	if math.Abs(mm-0.13) > 1e-9 {
		t.Errorf("expected 0.13, got %f", mm)
	}
}

func TestZAxisRecoupChannelSignaling(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:   make(chan struct{}, 1),
		bedLevelingGrid: make(map[string]any),
	}

	q.handleZAxisRecoup(map[string]any{"value": 42})

	select {
	case <-q.zAxisRecoupCh:
		// good
	case <-time.After(100 * time.Millisecond):
		t.Error("expected signal on zAxisRecoupCh")
	}
}

func TestSetZOffsetNoClient(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:   make(chan struct{}, 1),
		bedLevelingGrid: make(map[string]any),
	}
	v := 10
	q.zAxisRecoup = &v

	err := q.SetZOffset(context.Background(), 0.15)
	if err == nil {
		t.Error("expected error with no client")
	}
}

func TestSetZOffsetNoData(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:   make(chan struct{}, 1),
		bedLevelingGrid: make(map[string]any),
		client:          &fakeMQTTClient{},
	}

	err := q.SetZOffset(context.Background(), 0.15)
	if err == nil {
		t.Error("expected error when z-offset unknown")
	}
}

func TestSetZOffsetAlreadyAtTarget(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:   make(chan struct{}, 1),
		bedLevelingGrid: make(map[string]any),
		client:          &fakeMQTTClient{},
	}
	v := 13
	q.zAxisRecoup = &v

	err := q.SetZOffset(context.Background(), 0.13)
	if err != nil {
		t.Errorf("expected nil error for no-op, got %v", err)
	}
}

func TestNudgeZOffsetZeroDelta(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:   make(chan struct{}, 1),
		bedLevelingGrid: make(map[string]any),
		client:          &fakeMQTTClient{},
	}
	v := 13
	q.zAxisRecoup = &v

	err := q.NudgeZOffset(context.Background(), 0.0)
	if err != nil {
		t.Errorf("expected nil error for zero delta, got %v", err)
	}
}

func TestSetZOffsetWithReadback(t *testing.T) {
	fake := &fakeMQTTClient{}
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:   make(chan struct{}, 1),
		bedLevelingGrid: make(map[string]any),
		client:          fake,
	}
	v := 10
	q.zAxisRecoup = &v

	// Simulate the printer responding with ct=1021 after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		q.handleZAxisRecoup(map[string]any{"value": 15})
	}()

	err := q.SetZOffset(context.Background(), 0.15)
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}

	// Verify the GCode sent was correct.
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.commands) == 0 {
		t.Fatal("no gcode commands sent")
	}
	lastCmd := fake.commands[len(fake.commands)-1]
	cmdData, _ := lastCmd["cmdData"].(string)
	expected := "M290 Z+0.05"
	if cmdData != expected {
		t.Errorf("expected gcode %q, got %q", expected, cmdData)
	}
}

func TestSetZOffsetTimeout(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:   make(chan struct{}, 1),
		bedLevelingGrid: make(map[string]any),
		client:          &fakeMQTTClient{},
	}
	v := 10
	q.zAxisRecoup = &v

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := q.SetZOffset(ctx, 0.20)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestNudgeZOffsetWithReadback(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:   make(chan struct{}, 1),
		bedLevelingGrid: make(map[string]any),
		client:          &fakeMQTTClient{},
	}
	v := 10
	q.zAxisRecoup = &v

	go func() {
		time.Sleep(50 * time.Millisecond)
		// Current=10 + delta(0.03 = 3 steps) = 13
		q.handleZAxisRecoup(map[string]any{"value": 13})
	}()

	err := q.NudgeZOffset(context.Background(), 0.03)
	if err != nil {
		t.Errorf("expected success, got %v", err)
	}
}

func TestStepsConversion(t *testing.T) {
	tests := []struct {
		name   string
		steps  int
		wantMM float64
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

func TestMMToSteps(t *testing.T) {
	tests := []struct {
		name      string
		mm        float64
		wantSteps int
	}{
		{"zero", 0.0, 0},
		{"positive", 0.13, 13},
		{"negative", -0.05, -5},
		{"rounding up", 0.125, 13},
		{"rounding down", 0.124, 12},
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

func TestDeltaCalculation(t *testing.T) {
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

func TestZAxisRecoupInSnapshotState(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:         NewBaseWorker("test-mqtt"),
		zAxisRecoupCh:      make(chan struct{}, 1),
		bedLevelingGrid:    make(map[string]any),
		currentPrinterStat: -1,
	}

	// Without z-axis data
	snap := q.SnapshotState()
	if _, ok := snap["z_axis_recoup"]; ok {
		t.Error("z_axis_recoup should not be in snapshot when nil")
	}

	// With z-axis data
	v := 25
	q.zAxisRecoup = &v
	snap = q.SnapshotState()
	if snap["z_axis_recoup"] != 25 {
		t.Errorf("expected z_axis_recoup=25, got %v", snap["z_axis_recoup"])
	}
	if math.Abs(snap["z_offset_mm"].(float64)-0.25) > 1e-9 {
		t.Errorf("expected z_offset_mm=0.25, got %v", snap["z_offset_mm"])
	}
}
