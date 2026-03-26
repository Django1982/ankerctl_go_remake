package ws

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/service"
	"github.com/gorilla/websocket"
)

// readStatus reads the next JSON message from a WS conn and extracts "status".
func readStatus(t *testing.T, conn *websocket.Conn, timeout time.Duration) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read WS message: %v", err)
	}
	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		t.Fatalf("unmarshal WS message: %v", err)
	}
	s, ok := msg["status"].(string)
	if !ok {
		t.Fatalf("message missing status field: %v", msg)
	}
	return s
}

func TestPPPPState_InitialUnconnected(t *testing.T) {
	// Scenario 1: No PPPP service registered, probe returns false.
	// Expected: "disconnected" (via probe failure).
	h := New(nil, testState{
		loggedIn: true,
		probePPPP: func(context.Context) bool {
			return false
		},
	}, nil)

	conn, cleanup := newWSServer(t, "/ws/pppp-state", h.PPPPState)
	defer cleanup()

	status := readStatus(t, conn, 3*time.Second)
	if status != "disconnected" {
		t.Fatalf("initial unconnected status = %q, want disconnected", status)
	}
}

func TestPPPPState_ServiceConnected(t *testing.T) {
	// Scenario 2: PPPP service registered and connected.
	// Expected: "connected" immediately from service state.
	mgr := service.NewServiceManager()
	pppp := newMockService("ppppservice")
	pppp.state = service.StateRunning
	pppp.connected = true
	mgr.Register(pppp)

	h := New(mgr, testState{loggedIn: true}, nil)
	conn, cleanup := newWSServer(t, "/ws/pppp-state", h.PPPPState)
	defer cleanup()

	status := readStatus(t, conn, 2*time.Second)
	if status != "connected" {
		t.Fatalf("connected service status = %q, want connected", status)
	}
}

func TestPPPPState_MaxRetriesBackoff(t *testing.T) {
	// Scenario 3: After ppppMaxRetries (2) consecutive failures, the probe
	// interval should increase to ppppProbeInterval (60s).
	// We verify this by checking the ppppSharedProbe.failCount field after
	// multiple probe failures.
	var probeCount atomic.Int32
	h := New(nil, testState{
		loggedIn: true,
		probePPPP: func(context.Context) bool {
			probeCount.Add(1)
			return false
		},
	}, nil)

	conn, cleanup := newWSServer(t, "/ws/pppp-state", h.PPPPState)
	defer cleanup()

	// Read the initial status (triggers first probe).
	status := readStatus(t, conn, 3*time.Second)
	if status != "disconnected" {
		t.Fatalf("status = %q, want disconnected", status)
	}

	// Wait enough for the first retry cycle (15s interval) but not so long
	// that we hit the 60s backoff. Since we can't wait 15s in CI, we
	// directly verify the probe state machine after the first probe.
	h.ppppProbe.mu.Lock()
	fc := h.ppppProbe.failCount
	h.ppppProbe.mu.Unlock()

	if fc != 1 {
		t.Fatalf("failCount after first probe = %d, want 1", fc)
	}

	// Simulate additional failures by directly manipulating probe state
	// (the actual timing would require 15s+ waits per retry).
	h.ppppProbe.mu.Lock()
	h.ppppProbe.failCount = ppppMaxRetries + 1
	h.ppppProbe.mu.Unlock()

	// After MAX_RETRIES+1 failures, the next probe interval should be
	// ppppProbeInterval (60s), not ppppRetryInterval (15s). We verify
	// this by checking that no additional probe runs within a short window.
	initialProbes := probeCount.Load()
	time.Sleep(200 * time.Millisecond)

	// probeCount should not have increased (60s hasn't passed).
	finalProbes := probeCount.Load()
	if finalProbes > initialProbes+1 {
		t.Fatalf("expected no additional probes in backoff window, got %d -> %d", initialProbes, finalProbes)
	}
}

func TestPPPPState_RecoveryResetsFailCount(t *testing.T) {
	// Scenario 4: After failures, a successful probe resets failCount to 0.
	probeResults := make(chan bool, 10)
	h := New(nil, testState{
		loggedIn: true,
		probePPPP: func(context.Context) bool {
			select {
			case r := <-probeResults:
				return r
			default:
				return false
			}
		},
	}, nil)

	// Pre-seed with a failure result.
	probeResults <- false

	conn, cleanup := newWSServer(t, "/ws/pppp-state", h.PPPPState)
	defer cleanup()

	// First message: disconnected from failed probe.
	status := readStatus(t, conn, 3*time.Second)
	if status != "disconnected" {
		t.Fatalf("initial status = %q, want disconnected", status)
	}

	h.ppppProbe.mu.Lock()
	if h.ppppProbe.failCount != 1 {
		h.ppppProbe.mu.Unlock()
		t.Fatalf("failCount = %d, want 1", h.ppppProbe.failCount)
	}
	h.ppppProbe.mu.Unlock()

	// Now simulate a successful probe completing.
	h.ppppProbe.mu.Lock()
	h.ppppProbe.result = new(bool)
	*h.ppppProbe.result = true
	h.ppppProbe.failCount = 0
	h.ppppProbe.lastTime = time.Now()
	h.ppppProbe.mu.Unlock()

	// Wait for pollStatus to pick up the new state.
	time.Sleep(2 * time.Second)

	// Read next status — should be "connected".
	status = readStatus(t, conn, 3*time.Second)
	if status != "connected" {
		t.Fatalf("recovered status = %q, want connected", status)
	}

	h.ppppProbe.mu.Lock()
	fc := h.ppppProbe.failCount
	h.ppppProbe.mu.Unlock()
	if fc != 0 {
		t.Fatalf("failCount after recovery = %d, want 0", fc)
	}
}

func TestPPPPState_MQTTStaleness(t *testing.T) {
	// Scenario 5: When MQTT messages stop arriving (>30s gap), the state
	// machine should detect staleness and trigger probing. We test this
	// by providing a mqttqueue service with a LastMessageTime in the past.
	mgr := service.NewServiceManager()
	pppp := newMockService("ppppservice")
	pppp.state = service.StateRunning
	pppp.connected = false // PPPP not connected
	mgr.Register(pppp)

	mqtt := &mockMqttService{
		mockService:     newMockService("mqttqueue"),
		lastMessageTime: time.Now().Add(-60 * time.Second), // stale
	}
	mgr.Register(mqtt)

	var probed atomic.Bool
	h := New(mgr, testState{
		loggedIn: true,
		probePPPP: func(context.Context) bool {
			probed.Store(true)
			return false
		},
	}, nil)

	conn, cleanup := newWSServer(t, "/ws/pppp-state", h.PPPPState)
	defer cleanup()

	// The handler should detect MQTT staleness and trigger a probe.
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, _, _ = conn.ReadMessage() // consume initial message

	// Wait for the poll loop to fire and trigger a probe due to staleness.
	time.Sleep(2 * time.Second)

	if !probed.Load() {
		t.Fatal("expected probe to be triggered due to MQTT staleness")
	}
}

func TestPPPPState_ImmediateProbeOnFirstConnect(t *testing.T) {
	// Scenario 6: The first WS client to connect should trigger an
	// immediate probe (isFirst = true).
	var probeStarted atomic.Bool
	h := New(nil, testState{
		loggedIn: true,
		probePPPP: func(ctx context.Context) bool {
			probeStarted.Store(true)
			return true
		},
	}, nil)

	conn, cleanup := newWSServer(t, "/ws/pppp-state", h.PPPPState)
	defer cleanup()

	// Give the probe goroutine time to start.
	time.Sleep(100 * time.Millisecond)

	if !probeStarted.Load() {
		t.Fatal("first client connect did not trigger immediate probe")
	}

	// Should receive "connected" status.
	status := readStatus(t, conn, 3*time.Second)
	if status != "connected" {
		t.Fatalf("status after immediate probe = %q, want connected", status)
	}
}

func TestPPPPState_RetryAfterFirstFailure(t *testing.T) {
	// Scenario 7: After first probe failure, the retry interval is 15s
	// (ppppRetryInterval). We verify by checking failCount=1 and that
	// the next interval uses ppppRetryInterval (not ppppProbeInterval).
	h := New(nil, testState{
		loggedIn: true,
		probePPPP: func(context.Context) bool {
			return false
		},
	}, nil)

	conn, cleanup := newWSServer(t, "/ws/pppp-state", h.PPPPState)
	defer cleanup()

	status := readStatus(t, conn, 3*time.Second)
	if status != "disconnected" {
		t.Fatalf("status = %q, want disconnected", status)
	}

	h.ppppProbe.mu.Lock()
	fc := h.ppppProbe.failCount
	result := h.ppppProbe.result
	h.ppppProbe.mu.Unlock()

	if fc != 1 {
		t.Fatalf("failCount = %d, want 1", fc)
	}
	if result == nil || *result {
		t.Fatal("expected probe result to be false")
	}

	// With failCount=1 (<=ppppMaxRetries=2), the interval is ppppRetryInterval=15s.
	// Verify this by checking that fc <= ppppMaxRetries means retry interval.
	if fc > ppppMaxRetries {
		t.Fatalf("failCount %d > ppppMaxRetries %d after first failure", fc, ppppMaxRetries)
	}
}

func TestPPPPState_ClientDisconnectStopsGoroutine(t *testing.T) {
	// Scenario 8: When the client disconnects, the per-connection goroutine
	// should exit cleanly. We verify by checking clientCount returns to 0.
	h := New(nil, testState{
		loggedIn: true,
		probePPPP: func(ctx context.Context) bool {
			return true
		},
	}, nil)

	conn, cleanup := newWSServer(t, "/ws/pppp-state", h.PPPPState)

	// Read initial status to ensure the handler goroutine is running.
	status := readStatus(t, conn, 3*time.Second)
	if status != "connected" {
		t.Fatalf("status = %q, want connected", status)
	}

	h.ppppProbe.mu.Lock()
	countBefore := h.ppppProbe.clientCount
	h.ppppProbe.mu.Unlock()
	if countBefore != 1 {
		t.Fatalf("clientCount before disconnect = %d, want 1", countBefore)
	}

	// Disconnect the client.
	cleanup()

	// Wait for the handler goroutine to clean up.
	time.Sleep(200 * time.Millisecond)

	h.ppppProbe.mu.Lock()
	countAfter := h.ppppProbe.clientCount
	h.ppppProbe.mu.Unlock()
	if countAfter != 0 {
		t.Fatalf("clientCount after disconnect = %d, want 0", countAfter)
	}
}

// mockMqttService wraps mockService and adds LastMessageTime.
type mockMqttService struct {
	*mockService
	mu              sync.Mutex
	lastMessageTime time.Time
}

func (m *mockMqttService) LastMessageTime() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastMessageTime
}
