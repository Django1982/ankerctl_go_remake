package protocol

import "testing"

func TestCyclicU16EqualAndOverflow(t *testing.T) {
	if got := NewCyclicU16(0x42); got != CyclicU16(0x42) {
		t.Fatalf("expected 0x42, got %v", got)
	}
	if got := NewCyclicU16(0x10001); got != CyclicU16(0x1) {
		t.Fatalf("expected 0x1, got %v", got)
	}

	n := NewCyclicU16(0xFFFE)
	n = n.Add(1)
	if n != CyclicU16(0xFFFF) {
		t.Fatalf("expected 0xFFFF, got %v", n)
	}
	n = n.Add(1)
	if n != CyclicU16(0x0000) {
		t.Fatalf("expected 0x0000, got %v", n)
	}
	n = n.Add(1)
	if n != CyclicU16(0x0001) {
		t.Fatalf("expected 0x0001, got %v", n)
	}
}

func TestCyclicU16Less(t *testing.T) {
	tests := []struct {
		a, b CyclicU16
		lt   bool
	}{
		{0x1, 0x1, false},
		{0xFFFF, 0xFFFF, false},
		{0x1, 0x2, true},
		{0x2, 0x1, false},
		{0x90, 0x120, true},
		{0x120, 0x90, false},
		{0x101, 0x120, true},
		{0x120, 0x101, false},
		{0xFFFE, 0xFFFF, true},
		{0xFFFE, 0x10, true},
		{0xFFFE, 0x110, false},
	}

	for _, tc := range tests {
		if got := tc.a.Less(tc.b); got != tc.lt {
			t.Fatalf("%v < %v: expected %v got %v", tc.a, tc.b, tc.lt, got)
		}
	}
}

func TestCyclicU16Greater(t *testing.T) {
	tests := []struct {
		a, b CyclicU16
		gt   bool
	}{
		{0x1, 0x1, false},
		{0xFFFF, 0xFFFF, false},
		{0x2, 0x1, true},
		{0x1, 0x2, false},
		{0x120, 0x90, true},
		{0x90, 0x120, false},
		{0x120, 0x101, true},
		{0x101, 0x120, false},
		{0xFFFF, 0xFFFE, true},
		{0x10, 0xFFFE, true},
		{0x110, 0xFFFE, false},
	}

	for _, tc := range tests {
		if got := tc.a.Greater(tc.b); got != tc.gt {
			t.Fatalf("%v > %v: expected %v got %v", tc.a, tc.b, tc.gt, got)
		}
	}
}

func TestDiffAndAfter(t *testing.T) {
	a := CyclicU16(65530)
	b := CyclicU16(2)

	if d := Diff(a, b); d != 8 {
		t.Fatalf("expected diff 8, got %d", d)
	}
	if !IsAfter(a, b) {
		t.Fatalf("expected %v to be after %v", b, a)
	}
	if IsAfterOrEqual(b, a) {
		t.Fatalf("did not expect %v to be after %v", a, b)
	}
}
