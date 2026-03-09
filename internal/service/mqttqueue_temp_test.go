package service

import "testing"

func TestNormalizeTemp(t *testing.T) {
	tests := []struct {
		input int
		want  int
	}{
		{200, 200},      // Normal temp, unchanged.
		{0, 0},          // Zero.
		{1000, 1000},    // Edge: exactly 1000, unchanged.
		{1001, 10},      // Above 1000: divide by 100.
		{22000, 220},    // 22000 / 100 = 220.
		{6500, 65},      // 6500 / 100 = 65.
		{-10, -10},      // Negative, unchanged (shouldn't happen in practice).
	}
	for _, tt := range tests {
		got := normalizeTemp(tt.input)
		if got != tt.want {
			t.Errorf("normalizeTemp(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
