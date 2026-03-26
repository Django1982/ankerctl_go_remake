package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/mqtt/protocol"
)

func TestParseBLGrid_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantRows int
		wantCols int // max columns across rows
		spot     [3]float64 // [row, col, value] to verify
	}{
		{
			name: "standard 3x4 grid",
			input: `ok
BL-Grid-0 -0.767 -0.642 -0.512 -0.391
BL-Grid-1 -0.423 -0.311 -0.198 -0.087
BL-Grid-2  0.045  0.156  0.267  0.378
ok`,
			wantRows: 3,
			wantCols: 4,
			spot:     [3]float64{2, 3, 0.378},
		},
		{
			name: "single row",
			input: `BL-Grid-0 1.000 2.000 3.000
`,
			wantRows: 1,
			wantCols: 3,
			spot:     [3]float64{0, 1, 2.0},
		},
		{
			name: "5x5 grid with large values",
			input: `BL-Grid-0  0.100  0.200  0.300  0.400  0.500
BL-Grid-1 -1.100 -1.200 -1.300 -1.400 -1.500
BL-Grid-2  2.100  2.200  2.300  2.400  2.500
BL-Grid-3 -3.100 -3.200 -3.300 -3.400 -3.500
BL-Grid-4  4.100  4.200  4.300  4.400  4.500`,
			wantRows: 5,
			wantCols: 5,
			spot:     [3]float64{4, 4, 4.5},
		},
		{
			name:     "no grid data",
			input:    "no grid data here\nok\n",
			wantRows: 0,
			wantCols: 0,
		},
		{
			name:     "empty input",
			input:    "",
			wantRows: 0,
			wantCols: 0,
		},
		{
			name:     "malformed values ignored",
			input:    "BL-Grid-0 abc def ghi\n",
			wantRows: 0, // all values fail ParseFloat => empty row => skipped
			wantCols: 0,
		},
		{
			name: "mixed valid and invalid values",
			input: `BL-Grid-0 1.0 NaN 2.0
BL-Grid-1 3.0 4.0 5.0`,
			wantRows: 2,
			wantCols: 3, // first row has 2 valid + 1 invalid = 2 values
			spot:     [3]float64{1, 2, 5.0},
		},
		{
			name: "extra whitespace in values",
			input: `BL-Grid-0   0.100   0.200   0.300
BL-Grid-1   0.400   0.500   0.600`,
			wantRows: 2,
			wantCols: 3,
			spot:     [3]float64{0, 2, 0.3},
		},
		{
			name: "lines with non-grid content interleaved",
			input: `echo: ok
BL-Grid-0 1.0 2.0
some random text
BL-Grid-1 3.0 4.0
ok`,
			wantRows: 2,
			wantCols: 2,
			spot:     [3]float64{1, 0, 3.0},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			grid := parseBLGrid(tc.input)
			if len(grid) != tc.wantRows {
				t.Fatalf("rows = %d, want %d", len(grid), tc.wantRows)
			}
			if tc.wantRows == 0 {
				return
			}
			maxCols := 0
			for _, row := range grid {
				if len(row) > maxCols {
					maxCols = len(row)
				}
			}
			if maxCols != tc.wantCols {
				t.Fatalf("max cols = %d, want %d", maxCols, tc.wantCols)
			}
			if tc.spot != [3]float64{} {
				r, c := int(tc.spot[0]), int(tc.spot[1])
				if r < len(grid) && c < len(grid[r]) {
					if math.Abs(grid[r][c]-tc.spot[2]) > 1e-6 {
						t.Errorf("grid[%d][%d] = %v, want %v", r, c, grid[r][c], tc.spot[2])
					}
				}
			}
		})
	}
}

// TestLastBedLevelingGrid_StoreAndRetrieve verifies that handlePayload
// captures ct=1007 (MqttCmdAutoLeveling) grid data and that
// LastBedLevelingGrid returns a defensive copy.
func TestLastBedLevelingGrid_StoreAndRetrieve(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:         NewBaseWorker("mqttqueue"),
		bedLevelingGrid:    make(map[string]any),
		currentPrinterStat: -1,
	}
	q.BindHooks(q)

	// Initially empty.
	grid := q.LastBedLevelingGrid()
	if len(grid) != 0 {
		t.Fatalf("expected empty grid initially, got %d keys", len(grid))
	}

	// Simulate ct=1007 with a grid field.
	q.handlePayload(map[string]any{
		"commandType": int(protocol.MqttCmdAutoLeveling),
		"grid": map[string]any{
			"rows": 3,
			"cols": 4,
			"data": []float64{1.0, 2.0, 3.0},
		},
	})

	grid = q.LastBedLevelingGrid()
	if grid == nil {
		t.Fatal("expected grid after ct=1007, got nil")
	}
	if grid["rows"] != 3 {
		t.Fatalf("grid[rows] = %v, want 3", grid["rows"])
	}

	// Verify defensive copy: mutating returned map must not affect internal state.
	grid["rows"] = 999
	grid2 := q.LastBedLevelingGrid()
	if grid2["rows"] != 3 {
		t.Fatalf("internal grid mutated via returned map: rows = %v", grid2["rows"])
	}
}

// TestLastBedLevelingGrid_IgnoresNonGridPayload verifies that ct=1007
// without a "grid" field does not overwrite existing data.
func TestLastBedLevelingGrid_IgnoresNonGridPayload(t *testing.T) {
	q := &MqttQueue{
		BaseWorker:      NewBaseWorker("mqttqueue"),
		bedLevelingGrid: map[string]any{"rows": 5},
		currentPrinterStat: -1,
	}
	q.BindHooks(q)

	// ct=1007 without grid field.
	q.handlePayload(map[string]any{
		"commandType": int(protocol.MqttCmdAutoLeveling),
		"value":       0,
	})

	grid := q.LastBedLevelingGrid()
	if grid["rows"] != 5 {
		t.Fatalf("grid was overwritten despite missing grid field: %v", grid)
	}
}

// TestQueryBedLeveling_ParsesResponseData verifies the full QueryBedLeveling
// flow: sending M420 V, collecting ct=1043 responses via Tap, and parsing
// the BL-Grid text.
func TestQueryBedLeveling_ParsesResponseData(t *testing.T) {
	client := &fakeMQTTClient{}
	q := &MqttQueue{
		BaseWorker:         NewBaseWorker("mqttqueue"),
		client:             client,
		bedLevelingGrid:    make(map[string]any),
		currentPrinterStat: -1,
	}
	q.BindHooks(q)

	// Simulate the printer response arriving asynchronously.
	// We fire ct=1043 resData into the Tap handlers after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		// Simulate two response chunks arriving.
		q.Notify(map[string]any{
			"commandType": int(protocol.MqttCmdGcodeCommand),
			"resData":     "echo: ok\nBL-Grid-0 -0.100 -0.200 -0.300\nBL-Grid-1  0.400  0.500  0.600\n",
		})
		time.Sleep(20 * time.Millisecond)
		q.Notify(map[string]any{
			"commandType": int(protocol.MqttCmdGcodeCommand),
			"resData":     "BL-Grid-2  0.700  0.800  0.900\n",
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	result, err := q.QueryBedLeveling(ctx)
	if err != nil {
		t.Fatalf("QueryBedLeveling: %v", err)
	}

	// Validate result structure.
	grid, ok := result["grid"].([][]float64)
	if !ok {
		t.Fatalf("result[grid] type = %T, want [][]float64", result["grid"])
	}
	if len(grid) != 3 {
		t.Fatalf("grid rows = %d, want 3", len(grid))
	}
	if result["rows"] != 3 {
		t.Fatalf("result[rows] = %v, want 3", result["rows"])
	}
	if result["cols"] != 3 {
		t.Fatalf("result[cols] = %v, want 3", result["cols"])
	}
	if math.Abs(result["min"].(float64)-(-0.3)) > 1e-6 {
		t.Errorf("result[min] = %v, want -0.3", result["min"])
	}
	if math.Abs(result["max"].(float64)-0.9) > 1e-6 {
		t.Errorf("result[max] = %v, want 0.9", result["max"])
	}

	// Verify the grid is also persisted internally.
	last := q.LastBedLevelingGrid()
	if last["rows"] != 3 {
		t.Errorf("LastBedLevelingGrid rows = %v, want 3", last["rows"])
	}

	// Verify M420 V was sent.
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.commands) != 1 {
		t.Fatalf("commands sent = %d, want 1", len(client.commands))
	}
	if client.commands[0]["cmdData"] != "M420 V" {
		t.Fatalf("command = %v, want M420 V", client.commands[0]["cmdData"])
	}
}

// TestQueryBedLeveling_NoGridDataReturnsError verifies that QueryBedLeveling
// returns an error when the printer responds without BL-Grid lines.
func TestQueryBedLeveling_NoGridDataReturnsError(t *testing.T) {
	client := &fakeMQTTClient{}
	q := &MqttQueue{
		BaseWorker:         NewBaseWorker("mqttqueue"),
		client:             client,
		bedLevelingGrid:    make(map[string]any),
		currentPrinterStat: -1,
	}
	q.BindHooks(q)

	// Simulate response without BL-Grid lines.
	go func() {
		time.Sleep(50 * time.Millisecond)
		q.Notify(map[string]any{
			"commandType": int(protocol.MqttCmdGcodeCommand),
			"resData":     "echo: ok\nok\n",
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	_, err := q.QueryBedLeveling(ctx)
	if err == nil {
		t.Fatal("expected error for empty grid, got nil")
	}
	if want := "no BL-Grid data"; !containsSubstring(err.Error(), want) {
		t.Fatalf("error = %q, want substring %q", err.Error(), want)
	}
}

// TestQueryBedLeveling_CallerCancelledPropagatesError verifies that cancelling
// the parent context returns ctx.Err() rather than waiting the full 4s.
func TestQueryBedLeveling_CallerCancelledPropagatesError(t *testing.T) {
	client := &fakeMQTTClient{}
	q := &MqttQueue{
		BaseWorker:         NewBaseWorker("mqttqueue"),
		client:             client,
		bedLevelingGrid:    make(map[string]any),
		currentPrinterStat: -1,
	}
	q.BindHooks(q)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately after sending.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := q.QueryBedLeveling(ctx)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled (possibly wrapped)", err)
	}
}

// TestQueryBedLeveling_CmdResultFallback verifies that the cmdResult field
// is used when resData is absent (firmware revision compatibility).
func TestQueryBedLeveling_CmdResultFallback(t *testing.T) {
	client := &fakeMQTTClient{}
	q := &MqttQueue{
		BaseWorker:         NewBaseWorker("mqttqueue"),
		client:             client,
		bedLevelingGrid:    make(map[string]any),
		currentPrinterStat: -1,
	}
	q.BindHooks(q)

	go func() {
		time.Sleep(50 * time.Millisecond)
		q.Notify(map[string]any{
			"commandType": int(protocol.MqttCmdGcodeCommand),
			"cmdResult":   "BL-Grid-0 1.0 2.0\nBL-Grid-1 3.0 4.0\n",
		})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	result, err := q.QueryBedLeveling(ctx)
	if err != nil {
		t.Fatalf("QueryBedLeveling with cmdResult: %v", err)
	}
	if result["rows"] != 2 {
		t.Fatalf("rows = %v, want 2", result["rows"])
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
