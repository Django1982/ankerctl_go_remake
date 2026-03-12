package service

import (
	"math"
	"testing"
)

func TestParseBLGrid(t *testing.T) {
	input := `
ok
BL-Grid-0 -0.767 -0.642 -0.512 -0.391
BL-Grid-1 -0.423 -0.311 -0.198 -0.087
BL-Grid-2  0.045  0.156  0.267  0.378
ok
`
	grid := parseBLGrid(input)
	if len(grid) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(grid))
	}
	if len(grid[0]) != 4 {
		t.Fatalf("expected 4 cols, got %d", len(grid[0]))
	}
	if math.Abs(grid[0][0]-(-0.767)) > 1e-6 {
		t.Errorf("grid[0][0] = %v, want -0.767", grid[0][0])
	}
	if math.Abs(grid[2][3]-0.378) > 1e-6 {
		t.Errorf("grid[2][3] = %v, want 0.378", grid[2][3])
	}
}

func TestParseBLGridEmpty(t *testing.T) {
	grid := parseBLGrid("no grid data here\nok\n")
	if len(grid) != 0 {
		t.Errorf("expected empty grid, got %d rows", len(grid))
	}
}
