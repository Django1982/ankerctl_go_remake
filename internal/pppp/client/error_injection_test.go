package client

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/pppp/protocol"
)

// ---------------------------------------------------------------------------
// Recv — corrupted / partial packets
// ---------------------------------------------------------------------------

func TestRecv_CorruptedMagic(t *testing.T) {
	// Packet with wrong magic byte — DecodePacket must return an error.
	bad := []byte{0xDE, 0xD0, 0x00, 0x04, 0xD1, 0x01, 0x00, 0x01}
	mock := &mockUDPConn{
		reads: []queuedRead{{data: bad, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32100}}},
	}
	cli := NewClient(mock, protocol.Duid{}, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32100})

	_, _, err := cli.Recv(10 * time.Millisecond)
	if err == nil {
		t.Fatal("expected error for corrupted magic, got nil")
	}
	if !strings.Contains(err.Error(), "decode packet") {
		t.Fatalf("expected decode-packet wrapper error, got: %v", err)
	}
}

func TestRecv_PartialHeader(t *testing.T) {
	// Only 3 bytes — too short to be a valid PPPP header.
	bad := []byte{0xF1, 0xD0, 0x00}
	mock := &mockUDPConn{
		reads: []queuedRead{{data: bad, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32100}}},
	}
	cli := NewClient(mock, protocol.Duid{}, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32100})

	_, _, err := cli.Recv(10 * time.Millisecond)
	if err == nil {
		t.Fatal("expected error for partial header, got nil")
	}
}

func TestRecv_TruncatedPayload(t *testing.T) {
	// Header says length=10 but only 3 payload bytes are present.
	bad := make([]byte, 4+3) // 4-byte header + 3 bytes (length says 10)
	bad[0] = 0xF1
	bad[1] = byte(protocol.TypeDrw)
	bad[2] = 0x00
	bad[3] = 0x0A // length = 10, but only 3 bytes follow
	bad[4] = 0xD1
	bad[5] = 0x01
	bad[6] = 0x00

	mock := &mockUDPConn{
		reads: []queuedRead{{data: bad, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32100}}},
	}
	cli := NewClient(mock, protocol.Duid{}, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32100})

	_, _, err := cli.Recv(10 * time.Millisecond)
	if err == nil {
		t.Fatal("expected error for truncated payload, got nil")
	}
}

func TestRecv_EmptyDatagram(t *testing.T) {
	mock := &mockUDPConn{
		reads: []queuedRead{{data: []byte{}, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32100}}},
	}
	cli := NewClient(mock, protocol.Duid{}, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 32100})

	_, _, err := cli.Recv(10 * time.Millisecond)
	if err == nil {
		t.Fatal("expected error for empty datagram, got nil")
	}
}

// ---------------------------------------------------------------------------
// process — unknown Message types are handled gracefully (no panic)
// ---------------------------------------------------------------------------

func TestProcess_UnknownMessageTypesNoPanic(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 32100}
	mock := &mockUDPConn{}
	cli := NewClient(mock, protocol.Duid{Prefix: "EUPRAKM", Serial: 1, Check: "ABCDE"}, addr)
	cli.state = StateConnected

	// Raw Message types not handled by process() fall to the default case; no panic.
	for _, typ := range []protocol.Type{
		0x05, 0x07, 0x18, 0x50, 0x99, 0xBB,
	} {
		cli.process(protocol.Message{Type: typ, Payload: []byte{0x01}})
	}
}

// ---------------------------------------------------------------------------
// process — DRW with out-of-range channel index must not panic
// ---------------------------------------------------------------------------

func TestProcess_DrwOutOfBoundsChannel(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 32100}
	mock := &mockUDPConn{}
	cli := NewClient(mock, protocol.Duid{}, addr)
	cli.state = StateConnected

	// Channel 8 is out of range (valid: 0–7); must not panic.
	cli.process(protocol.Drw{Chan: 8, Index: 0, Data: []byte("test")})
}

func TestProcess_DrwAckOutOfBoundsChannel(t *testing.T) {
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 32100}
	mock := &mockUDPConn{}
	cli := NewClient(mock, protocol.Duid{}, addr)
	cli.state = StateConnected

	// Channel 255 is out of range; must not panic.
	cli.process(protocol.DrwAck{Chan: 255, Acks: []uint16{0, 1}})
}

// ---------------------------------------------------------------------------
// process — PunchPkt from different DUID is ignored
// ---------------------------------------------------------------------------

func TestProcess_PunchPktDUIDFilterIgnoresWrongPrinter(t *testing.T) {
	myDUID := protocol.Duid{Prefix: "EUPRAKM", Serial: 100001, Check: "AAAAA"}
	otherDUID := protocol.Duid{Prefix: "EUPRAKM", Serial: 999999, Check: "ZZZZZ"}

	addr := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 1), Port: 32100}
	mock := &mockUDPConn{}
	cli := NewClient(mock, myDUID, addr)
	cli.state = StateConnecting

	// PunchPkt from the wrong printer DUID must not trigger any outbound packet.
	cli.process(protocol.PunchPkt{DUID: otherDUID})

	mock.mu.Lock()
	nWrites := len(mock.writes)
	mock.mu.Unlock()

	if nWrites != 0 {
		t.Fatalf("expected 0 writes for wrong-DUID PunchPkt, got %d", nWrites)
	}
}

// ---------------------------------------------------------------------------
// SendPacket — missing remote addr returns error without panic
// ---------------------------------------------------------------------------

func TestSendPacket_NoRemoteAddr(t *testing.T) {
	mock := &mockUDPConn{}
	cli := NewClient(mock, protocol.Duid{}, nil)

	err := cli.SendPacket(protocol.PingReq{}, nil)
	if err == nil {
		t.Fatal("expected error for missing remote addr, got nil")
	}
	if !strings.Contains(err.Error(), "missing remote address") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Run — corrupted inbound packet is logged/skipped, Run continues
// ---------------------------------------------------------------------------

func TestRun_CorruptedPacketDoesNotTerminate(t *testing.T) {
	// Queue one corrupted datagram followed by a Close packet.
	// Run must survive the corrupted datagram and then return ErrConnectionReset.
	corrupt := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x00, 0x00, 0x00}
	closeRaw, err := protocol.EncodePacket(protocol.Close{})
	if err != nil {
		t.Fatalf("encode Close: %v", err)
	}
	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 32100}
	mock := &mockUDPConn{
		reads: []queuedRead{
			{data: corrupt, addr: addr},
			{data: closeRaw, addr: addr},
		},
	}
	cli := NewClient(mock, protocol.Duid{Prefix: "ABCDEF", Serial: 1, Check: "QWERT"}, addr)
	cli.state = StateConnected

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	runErr := cli.Run(ctx)
	if runErr != ErrConnectionReset {
		t.Fatalf("expected ErrConnectionReset, got %v", runErr)
	}
}

// ---------------------------------------------------------------------------
// Run — multiple consecutive corrupted packets, Run exits on context cancel
// ---------------------------------------------------------------------------

func TestRun_MultipleCorruptedPacketsNoPanic(t *testing.T) {
	corrupt1 := []byte{0xDE, 0xAD}
	corrupt2 := []byte{0x00, 0x00, 0x00, 0x00}
	corrupt3 := []byte{0xF1, 0x00} // valid magic but too short

	addr := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 32100}
	mock := &mockUDPConn{
		reads: []queuedRead{
			{data: corrupt1, addr: addr},
			{data: corrupt2, addr: addr},
			{data: corrupt3, addr: addr},
		},
	}
	cli := NewClient(mock, protocol.Duid{Prefix: "ABCDEF", Serial: 1, Check: "QWERT"}, addr)
	cli.state = StateConnected

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	// Run should exit due to context timeout (nil), not panic.
	runErr := cli.Run(ctx)
	if runErr != nil {
		t.Fatalf("expected nil from context cancel, got %v", runErr)
	}
}
