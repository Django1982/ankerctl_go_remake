package protocol

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Channel — DRW in-flight window (64-packet hard limit)
// ---------------------------------------------------------------------------

func TestChannel_MaxInFlightWindowEnforced(t *testing.T) {
	ch := NewChannel(0)

	// Write 65 × 1024-byte chunks so all end up in backlog.
	big := make([]byte, 1024*65)
	if _, _, err := ch.Write(big, false); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// First Poll must release exactly DefaultMaxInFlight (64) packets.
	pkts := ch.Poll(time.Now())
	if len(pkts) != DefaultMaxInFlight {
		t.Fatalf("expected %d packets in-flight, got %d", DefaultMaxInFlight, len(pkts))
	}

	// A second Poll (no time advance, no ACKs) must not release new indices.
	// Retransmits of already-queued packets are fine, but index 64 must NOT appear.
	pkts2 := ch.Poll(time.Now())
	for _, p := range pkts2 {
		if p.Index >= uint16(DefaultMaxInFlight) {
			t.Fatalf("unexpected new packet index %d while window is full", p.Index)
		}
	}
}

func TestChannel_InFlightWindowReleaseAfterACK(t *testing.T) {
	ch := NewChannel(0)
	ch.maxInFlight = 3

	// Enqueue 5 packets.
	ch.Write(make([]byte, 1024*5), false)

	// Poll fills the window to 3.
	first := ch.Poll(time.Now())
	if len(first) != 3 {
		t.Fatalf("expected 3 initial packets, got %d", len(first))
	}

	// ACK the first 2; window has room for 2 more backlog items.
	ch.RXAck([]uint16{0, 1})
	second := ch.Poll(time.Now())

	newCount := 0
	for _, p := range second {
		if p.Index >= 3 {
			newCount++
		}
	}
	if newCount < 2 {
		t.Fatalf("expected at least 2 new packets after partial ACK, got %d", newCount)
	}
}

// ---------------------------------------------------------------------------
// Channel — out-of-order ACK processing (RXAck with unsorted indices)
// ---------------------------------------------------------------------------

func TestChannel_OutOfOrderAcks(t *testing.T) {
	ch := NewChannel(1)

	// Enqueue and transmit 3 packets (indices 0, 1, 2).
	ch.Write([]byte("pkt0"), false)
	ch.Write([]byte("pkt1"), false)
	ch.Write([]byte("pkt2"), false)
	ch.Poll(time.Now()) // flush backlog → txQueue

	// ACK index 2 first (out of order), then 1, then 0.
	ch.RXAck([]uint16{2})
	ch.RXAck([]uint16{1})
	ch.RXAck([]uint16{0})

	// txAck should have advanced past all three: next expected = 3.
	if ch.txAck != CyclicU16(3) {
		t.Fatalf("txAck = %v, want 3", ch.txAck)
	}
}

func TestChannel_DuplicateACKIgnored(t *testing.T) {
	ch := NewChannel(2)
	ch.Write([]byte("data"), false)
	ch.Poll(time.Now())

	ch.RXAck([]uint16{0}) // first ACK
	ch.RXAck([]uint16{0}) // duplicate — must not advance txAck twice

	if ch.txAck != CyclicU16(1) {
		t.Fatalf("txAck = %v after duplicate ack, want 1", ch.txAck)
	}
}

// ---------------------------------------------------------------------------
// Channel — RXDrw wraparound (uint16 sequence number rollover)
// ---------------------------------------------------------------------------

func TestChannel_RxSeqRollover(t *testing.T) {
	ch := NewChannel(3)

	// Advance rxCtr to 0xFFFE so that the next expected indices wrap around.
	ch.rxCtr = CyclicU16(0xFFFE)

	// Deliver index 0x0000 before 0xFFFE (out of order across the wrap boundary).
	ch.RXDrw(0x0000, []byte("wrap"))
	// rxCtr is 0xFFFE, index 0x0000 is not yet deliverable — buffer it.
	if got := ch.Read(4, 0); got != nil {
		t.Fatalf("unexpected data before 0xFFFE delivered, got %q", got)
	}

	// Deliver 0xFFFE — rxCtr advances to 0xFFFF; 0x0000 still buffered.
	ch.RXDrw(0xFFFE, []byte("pre1"))
	if got := string(ch.Read(4, time.Millisecond)); got != "pre1" {
		t.Fatalf("expected 'pre1', got %q", got)
	}

	// Deliver 0xFFFF — rxCtr wraps to 0x0000; buffered 'wrap' is released.
	ch.RXDrw(0xFFFF, []byte("pre2"))
	if got := string(ch.Read(4, time.Millisecond)); got != "pre2" {
		t.Fatalf("expected 'pre2', got %q", got)
	}

	if got := string(ch.Read(4, time.Millisecond)); got != "wrap" {
		t.Fatalf("expected 'wrap' after rollover delivery, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Channel — stale/old RXDrw packets (index far behind rxCtr) are dropped
// ---------------------------------------------------------------------------

func TestChannel_StaleRxDrwDropped(t *testing.T) {
	ch := NewChannel(4)

	// Advance rxCtr to 200.
	ch.rxCtr = CyclicU16(200)

	// Deliver a packet with index 10 — rxCtr.Greater(10) is true,
	// and the age (200-10=190) exceeds maxAgeWarn (128) → silently dropped.
	ch.RXDrw(10, []byte("stale"))

	if got := ch.Read(5, 0); got != nil {
		t.Fatalf("expected stale packet to be dropped, got %q", got)
	}
}

func TestChannel_SlightlyOldRxDrwDropped(t *testing.T) {
	ch := NewChannel(5)

	// Set rxCtr to 50; deliver index 10 (age = 40, within maxAgeWarn=128).
	// Per RXDrw implementation: both cases (> maxAgeWarn AND ≤ maxAgeWarn)
	// return without adding to rxQueue when rxCtr.Greater(idx) is true.
	ch.rxCtr = CyclicU16(50)
	ch.RXDrw(10, []byte("old"))

	if got := ch.Read(3, 0); got != nil {
		t.Fatalf("expected old packet to be dropped, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// CyclicU16 — wraparound arithmetic edge cases
// ---------------------------------------------------------------------------

func TestCyclicU16_DiffWraparound(t *testing.T) {
	tests := []struct {
		name      string
		a, b      CyclicU16
		wantDiff  uint16
		wantAfter bool
	}{
		{"0xFFFE to 0x0002 wraps", 0xFFFE, 0x0002, 4, true},
		{"0xFFFF to 0x0000 wraps", 0xFFFF, 0x0000, 1, true},
		{"equal values", 0x0005, 0x0005, 0, false},
		{"normal forward", 0x0010, 0x0012, 2, true},
		{"0xFF80 to 0x0080 wraps 256", 0xFF80, 0x0080, 0x0100, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Diff(tc.a, tc.b)
			if got != tc.wantDiff {
				t.Errorf("Diff(%v, %v) = %d, want %d", tc.a, tc.b, got, tc.wantDiff)
			}
			after := IsAfter(tc.a, tc.b)
			if after != tc.wantAfter {
				t.Errorf("IsAfter(%v, %v) = %v, want %v", tc.a, tc.b, after, tc.wantAfter)
			}
		})
	}
}

func TestCyclicU16_IsAfterOrEqualAtWrap(t *testing.T) {
	// a == b → IsAfterOrEqual must return true.
	if !IsAfterOrEqual(CyclicU16(0), CyclicU16(0)) {
		t.Error("IsAfterOrEqual(0, 0) should be true")
	}
	if !IsAfterOrEqual(CyclicU16(0xFFFF), CyclicU16(0xFFFF)) {
		t.Error("IsAfterOrEqual(0xFFFF, 0xFFFF) should be true")
	}
	// b is one step ahead of a → true.
	if !IsAfterOrEqual(CyclicU16(0xFFFF), CyclicU16(0x0000)) {
		t.Error("IsAfterOrEqual(0xFFFF, 0x0000) should be true (0x0000 is one ahead)")
	}
}

func TestCyclicU16_SubWrap(t *testing.T) {
	c := NewCyclicU16(0)
	c = c.Sub(1)
	if c != CyclicU16(0xFFFF) {
		t.Fatalf("0 - 1 should wrap to 0xFFFF, got %v", c)
	}
}

// ---------------------------------------------------------------------------
// Wire — Read/Peek timeout returns nil (no panic)
// ---------------------------------------------------------------------------

func TestWire_ReadTimeout(t *testing.T) {
	w := NewWire()
	// Request 10 bytes with a very short timeout — should return nil, not block forever.
	got := w.Read(10, time.Millisecond)
	if got != nil {
		t.Fatalf("expected nil on read timeout, got %q", got)
	}
}

func TestWire_PeekTimeout(t *testing.T) {
	w := NewWire()
	got := w.Peek(10, time.Millisecond)
	if got != nil {
		t.Fatalf("expected nil on peek timeout, got %q", got)
	}
}

func TestWire_ReadZeroByteRequest(t *testing.T) {
	w := NewWire()
	w.Write([]byte("abc"))
	// Reading 0 bytes is a degenerate case — must not panic.
	got := w.Read(0, 0)
	if got == nil {
		t.Fatal("expected non-nil slice for 0-byte read from non-empty wire")
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d bytes", len(got))
	}
}
