package protocol

import (
	"testing"
	"time"
)

func TestChannelRXReorder(t *testing.T) {
	ch := NewChannel(1)

	ch.RXDrw(1, []byte("world"))
	if got := ch.Read(5, 0); got != nil {
		t.Fatalf("expected no output before packet 0, got %q", got)
	}

	ch.RXDrw(0, []byte("hello"))
	if got := string(ch.Read(5, time.Millisecond)); got != "hello" {
		t.Fatalf("expected hello, got %q", got)
	}
	if got := string(ch.Read(5, time.Millisecond)); got != "world" {
		t.Fatalf("expected world, got %q", got)
	}
}

func TestChannelPollAndAck(t *testing.T) {
	ch := NewChannel(2)
	ch.timeout = 20 * time.Millisecond

	start, done, err := ch.Write([]byte("abcdef"), false)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if start != 0 || done != 1 {
		t.Fatalf("unexpected counters start=%v done=%v", start, done)
	}

	pkts := ch.Poll(time.Now())
	if len(pkts) != 1 {
		t.Fatalf("expected 1 packet from poll, got %d", len(pkts))
	}
	if string(pkts[0].Data) != "abcdef" {
		t.Fatalf("unexpected payload: %q", pkts[0].Data)
	}

	ch.RXAck([]uint16{0})
	if ch.txAck != 1 {
		t.Fatalf("expected txAck=1, got %v", ch.txAck)
	}
}

func TestChannelInFlightWindow(t *testing.T) {
	ch := NewChannel(3)
	ch.maxInFlight = 2

	data := make([]byte, 1024*4)
	if _, _, err := ch.Write(data, false); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	pkts := ch.Poll(time.Now())
	if len(pkts) != 2 {
		t.Fatalf("expected 2 packets due to in-flight window, got %d", len(pkts))
	}

	ch.RXAck([]uint16{0, 1})
	pkts = ch.Poll(time.Now())
	if len(pkts) == 0 {
		t.Fatalf("expected additional packets after ack")
	}
}

func TestChannelRetransmit(t *testing.T) {
	ch := NewChannel(4)
	ch.timeout = 10 * time.Millisecond

	if _, _, err := ch.Write([]byte("abc"), false); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	first := ch.Poll(time.Now())
	if len(first) != 1 {
		t.Fatalf("expected first transmission")
	}

	time.Sleep(15 * time.Millisecond)
	second := ch.Poll(time.Now())
	if len(second) != 1 {
		t.Fatalf("expected retransmission, got %d", len(second))
	}
}
