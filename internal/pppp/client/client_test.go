package client

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/pppp/protocol"
)

type queuedRead struct {
	data []byte
	addr *net.UDPAddr
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

type mockUDPConn struct {
	mu       sync.Mutex
	deadline time.Time
	reads    []queuedRead
	writes   []queuedRead
	closed   bool
}

func (m *mockUDPConn) SetReadDeadline(t time.Time) error {
	m.mu.Lock()
	m.deadline = t
	m.mu.Unlock()
	return nil
}

func (m *mockUDPConn) ReadFromUDP(b []byte) (int, *net.UDPAddr, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, nil, net.ErrClosed
	}
	if len(m.reads) == 0 {
		if !m.deadline.IsZero() && time.Now().After(m.deadline) {
			return 0, nil, timeoutErr{}
		}
		return 0, nil, timeoutErr{}
	}
	next := m.reads[0]
	m.reads = m.reads[1:]
	copy(b, next.data)
	return len(next.data), next.addr, nil
}

func (m *mockUDPConn) WriteToUDP(b []byte, addr *net.UDPAddr) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, net.ErrClosed
	}
	data := make([]byte, len(b))
	copy(data, b)
	m.writes = append(m.writes, queuedRead{data: data, addr: addr})
	return len(b), nil
}

func (m *mockUDPConn) Close() error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	return nil
}

func TestDiscoverLANIPWithConn(t *testing.T) {
	expected := protocol.Duid{Prefix: "ABCDEF", Serial: 123456, Check: "QWERT"}
	resp, err := protocol.EncodePacket(protocol.PunchPkt{DUID: expected})
	if err != nil {
		t.Fatalf("encode response failed: %v", err)
	}

	mock := &mockUDPConn{
		reads: []queuedRead{{data: resp, addr: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: PPPPLANPort}}},
	}
	cli := NewClient(mock, protocol.Duid{}, &net.UDPAddr{IP: net.IPv4bcast, Port: PPPPLANPort})
	cli.state = StateConnected

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ip, err := discoverLANIPWithConn(ctx, cli, expected.String())
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if !ip.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("expected 127.0.0.1, got %v", ip)
	}
	if len(mock.writes) != 1 {
		t.Fatalf("expected 1 outbound packet, got %d", len(mock.writes))
	}
	decoded, err := protocol.DecodePacket(mock.writes[0].data)
	if err != nil {
		t.Fatalf("decode write failed: %v", err)
	}
	if _, ok := decoded.(protocol.LanSearch); !ok {
		t.Fatalf("expected LanSearch write, got %T", decoded)
	}
}

func TestRunSendsCloseOnContextCancel(t *testing.T) {
	// Verify that Run() sends a Close packet when the context is cancelled,
	// matching Python's ppppapi.py run() which always sends PktClose() on exit.
	mock := &mockUDPConn{}
	addr := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 50), Port: PPPPLANPort}
	cli := NewClient(mock, protocol.Duid{Prefix: "ABCDEF", Serial: 1, Check: "QWERT"}, addr)
	cli.state = StateConnecting

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after a brief delay so Run processes at least one tick.
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	err := cli.Run(ctx)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	// Check that the last write was a Close packet.
	mock.mu.Lock()
	writes := mock.writes
	mock.mu.Unlock()
	if len(writes) == 0 {
		t.Fatal("expected at least one write (Close), got 0")
	}
	last := writes[len(writes)-1]
	decoded, err := protocol.DecodePacket(last.data)
	if err != nil {
		t.Fatalf("decode last write failed: %v", err)
	}
	if _, ok := decoded.(protocol.Close); !ok {
		t.Fatalf("expected Close packet as last write, got %T", decoded)
	}
	if cli.State() != StateDisconnected {
		t.Fatalf("expected StateDisconnected after Run, got %v", cli.State())
	}
}

func TestRunReturnsErrConnectionResetOnRemoteClose(t *testing.T) {
	// Verify that Run() returns ErrConnectionReset when a remote Close is received.
	closePacket, err := protocol.EncodePacket(protocol.Close{})
	if err != nil {
		t.Fatalf("encode Close: %v", err)
	}
	addr := &net.UDPAddr{IP: net.IPv4(192, 168, 1, 50), Port: PPPPLANPort}
	mock := &mockUDPConn{
		reads: []queuedRead{{data: closePacket, addr: addr}},
	}
	cli := NewClient(mock, protocol.Duid{Prefix: "ABCDEF", Serial: 1, Check: "QWERT"}, addr)
	cli.state = StateConnected

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	runErr := cli.Run(ctx)
	if !errors.Is(runErr, ErrConnectionReset) {
		t.Fatalf("expected ErrConnectionReset, got %v", runErr)
	}
}

func TestDiscoverLANIPTimeout(t *testing.T) {
	mock := &mockUDPConn{}
	cli := NewClient(mock, protocol.Duid{}, &net.UDPAddr{IP: net.IPv4bcast, Port: PPPPLANPort})
	cli.state = StateConnected

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := discoverLANIPWithConn(ctx, cli, "")
	if err == nil {
		t.Fatalf("expected context timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", err)
	}
}
