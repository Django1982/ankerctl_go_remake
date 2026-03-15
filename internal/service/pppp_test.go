package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ppppclient "github.com/django1982/ankerctl/internal/pppp/client"
	"github.com/django1982/ankerctl/internal/pppp/protocol"
)

type fakePPPPConn struct {
	mu       sync.Mutex
	runErr   error
	state    ppppclient.State
	chans    [8]*protocol.Channel
	closed   atomic.Bool
	remoteIP net.IP
	// runDelay controls how long Run blocks before returning runErr.
	runDelay time.Duration
	// channelErr, if set, is returned from Channel() for all indices.
	channelErr error
}

func newFakePPPPConn() *fakePPPPConn {
	f := &fakePPPPConn{state: ppppclient.StateConnected}
	for i := 0; i < len(f.chans); i++ {
		f.chans[i] = protocol.NewChannel(uint8(i))
	}
	return f
}

func (f *fakePPPPConn) ConnectLANSearch() error { return nil }
func (f *fakePPPPConn) Run(ctx context.Context) error {
	delay := f.runDelay
	if delay == 0 {
		delay = 5 * time.Millisecond
	}
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(delay):
		return f.runErr
	}
}
func (f *fakePPPPConn) Close() error {
	f.closed.Store(true)
	return nil
}
func (f *fakePPPPConn) RemoteIP() net.IP {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.remoteIP
}
func (f *fakePPPPConn) State() ppppclient.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state
}
func (f *fakePPPPConn) setState(s ppppclient.State) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state = s
}
func (f *fakePPPPConn) Channel(index int) (*protocol.Channel, error) {
	if f.channelErr != nil {
		return nil, f.channelErr
	}
	if index < 0 || index >= len(f.chans) {
		return nil, errors.New("index out of range")
	}
	return f.chans[index], nil
}

func TestPPPPService_ConnectionResetTriggersRestart(t *testing.T) {
	fake := newFakePPPPConn()
	fake.runErr = errors.New("connection reset by peer")

	svc := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.Default(),
		clientFactor: func(context.Context) (ppppConn, error) { return fake, nil },
		pollInterval: 1 * time.Millisecond,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}
	svc.BindHooks(svc)

	if err := svc.WorkerStart(); err != nil {
		t.Fatalf("WorkerStart: %v", err)
	}
	defer svc.WorkerStop()

	err := svc.WorkerRun(context.Background())
	if !IsServiceRestartSignal(err) {
		t.Fatalf("WorkerRun err = %v, want ServiceRestartSignal", err)
	}
}

func TestPPPPService_P2PCommandUsesPythonShape(t *testing.T) {
	fake := newFakePPPPConn()
	svc := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.Default(),
		client:       fake,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}

	if err := svc.P2PCommand(context.Background(), protocol.P2PSubCmdLightStateSwitch, map[string]any{"open": true}); err != nil {
		t.Fatalf("P2PCommand: %v", err)
	}

	ch, err := fake.Channel(0)
	if err != nil {
		t.Fatalf("Channel(0): %v", err)
	}
	drws := ch.Poll(time.Now())
	if len(drws) == 0 {
		t.Fatal("expected at least one DRW packet")
	}
	x, err := protocol.ParseXzyh(drws[0].Data)
	if err != nil {
		t.Fatalf("ParseXzyh: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(x.Data, &payload); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if payload["commandType"] != float64(protocol.P2PSubCmdLightStateSwitch) {
		t.Fatalf("commandType=%v, want %d", payload["commandType"], protocol.P2PSubCmdLightStateSwitch)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing nested data payload: %#v", payload)
	}
	if data["open"] != true {
		t.Fatalf("data.open=%v, want true", data["open"])
	}
}

func TestProbePPPPWithFactoryConnected(t *testing.T) {
	fake := newFakePPPPConn()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ok := probePPPPWithFactory(ctx, func(context.Context) (ppppConn, error) {
		return fake, nil
	})
	if !ok {
		t.Fatal("probePPPPWithFactory() = false, want true")
	}
}

func TestProbePPPPWithFactoryFailure(t *testing.T) {
	fake := newFakePPPPConn()
	fake.state = ppppclient.StateDisconnected
	fake.runErr = errors.New("probe failed")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ok := probePPPPWithFactory(ctx, func(context.Context) (ppppConn, error) {
		return fake, nil
	})
	if ok {
		t.Fatal("probePPPPWithFactory() = true, want false")
	}
}

// newTestPPPPService creates a PPPPService wired to a fake conn and factory
// without requiring a real config.Manager.
func newTestPPPPService(fake *fakePPPPConn) *PPPPService {
	svc := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.Default(),
		clientFactor: func(context.Context) (ppppConn, error) { return fake, nil },
		pollInterval: 1 * time.Millisecond,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}
	svc.BindHooks(svc)
	return svc
}

func TestPPPPService_HandshakeConnectTimeout(t *testing.T) {
	// Simulate a printer that never transitions to StateConnected. WorkerRun
	// should return ErrServiceRestartSignal after the connectTimeout expires.
	fake := newFakePPPPConn()
	fake.state = ppppclient.StateConnecting
	// Run blocks indefinitely (until ctx cancelled) to prevent the Run-error
	// path from triggering the restart before the timeout does.
	fake.runDelay = 30 * time.Second

	svc := newTestPPPPService(fake)
	if err := svc.WorkerStart(); err != nil {
		t.Fatalf("WorkerStart: %v", err)
	}
	defer svc.WorkerStop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	start := time.Now()
	err := svc.WorkerRun(ctx)
	elapsed := time.Since(start)

	if !IsServiceRestartSignal(err) {
		t.Fatalf("WorkerRun err = %v, want ServiceRestartSignal", err)
	}
	// connectTimeout is 10s; verify we didn't wait the full ctx timeout (15s).
	if elapsed > 12*time.Second {
		t.Fatalf("took %v to timeout, expected ~10s (connectTimeout)", elapsed)
	}
}

func TestPPPPService_HandshakeTransitionToConnected(t *testing.T) {
	// Simulate a printer that starts in StateConnecting and transitions to
	// StateConnected after a short delay, mimicking the LanSearch → PunchPkt
	// → P2pRdy handshake. WorkerRun should proceed normally after connection.
	fake := newFakePPPPConn()
	fake.state = ppppclient.StateConnecting
	fake.runDelay = 30 * time.Second // keep Run alive

	svc := newTestPPPPService(fake)
	if err := svc.WorkerStart(); err != nil {
		t.Fatalf("WorkerStart: %v", err)
	}
	defer svc.WorkerStop()

	// After 50ms, transition to connected.
	go func() {
		time.Sleep(50 * time.Millisecond)
		fake.setState(ppppclient.StateConnected)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// WorkerRun should block until ctx is cancelled (since Run stays alive and
	// the connection is healthy). Returning nil means clean shutdown.
	err := svc.WorkerRun(ctx)
	if err != nil && !IsServiceRestartSignal(err) {
		t.Fatalf("WorkerRun err = %v, want nil or restart", err)
	}
}

func TestPPPPService_FactoryError(t *testing.T) {
	// If the factory returns an error, WorkerStart must fail.
	svc := &PPPPService{
		BaseWorker: NewBaseWorker("ppppservice"),
		log:        slog.Default(),
		clientFactor: func(context.Context) (ppppConn, error) {
			return nil, errors.New("no printer found")
		},
		pollInterval: 1 * time.Millisecond,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}
	svc.BindHooks(svc)

	err := svc.WorkerStart()
	if err == nil {
		t.Fatal("expected WorkerStart error, got nil")
	}
}

func TestPPPPService_NoClientWorkerRun(t *testing.T) {
	// WorkerRun with no client set must return an error immediately.
	svc := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.Default(),
		pollInterval: 1 * time.Millisecond,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}
	svc.BindHooks(svc)
	// client is nil — no WorkerStart called.

	err := svc.WorkerRun(context.Background())
	if err == nil {
		t.Fatal("expected error from WorkerRun with nil client")
	}
}

func TestPPPPService_P2PCommandNoClient(t *testing.T) {
	// P2PCommand with no client must return an error.
	svc := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.Default(),
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}

	err := svc.P2PCommand(context.Background(), protocol.P2PSubCmdStartLive, nil)
	if err == nil {
		t.Fatal("expected error from P2PCommand with no client")
	}
}

func TestPPPPService_ChannelErrorDuringDrain(t *testing.T) {
	// If Channel() returns an error during drainAllXzyh, WorkerRun should
	// return ErrServiceRestartSignal.
	fake := newFakePPPPConn()
	fake.channelErr = errors.New("channel gone")

	svc := newTestPPPPService(fake)
	if err := svc.WorkerStart(); err != nil {
		t.Fatalf("WorkerStart: %v", err)
	}
	defer svc.WorkerStop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := svc.WorkerRun(ctx)
	if !IsServiceRestartSignal(err) {
		t.Fatalf("WorkerRun err = %v, want ServiceRestartSignal", err)
	}
}

func TestPPPPService_IsConnected(t *testing.T) {
	tests := []struct {
		name     string
		state    ppppclient.State
		hasConn  bool
		expected bool
	}{
		{"connected", ppppclient.StateConnected, true, true},
		{"connecting", ppppclient.StateConnecting, true, false},
		{"disconnected", ppppclient.StateDisconnected, true, false},
		{"no client", ppppclient.StateConnected, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &PPPPService{
				BaseWorker:   NewBaseWorker("ppppservice"),
				log:          slog.Default(),
				handlers:     make(map[byte][]func([]byte)),
				aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
			}
			if tt.hasConn {
				fake := newFakePPPPConn()
				fake.state = tt.state
				svc.client = fake
			}
			if got := svc.IsConnected(); got != tt.expected {
				t.Fatalf("IsConnected() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestPPPPService_XzyhHandlerDispatch(t *testing.T) {
	// Register an XZYH handler on channel 2 and verify it receives dispatched
	// payloads. This tests the handler registration + dispatch path without
	// needing real network data.
	fake := newFakePPPPConn()
	svc := newTestPPPPService(fake)

	var received []byte
	var mu sync.Mutex
	svc.RegisterXzyhHandler(2, func(data []byte) {
		mu.Lock()
		received = append([]byte(nil), data...)
		mu.Unlock()
	})

	payload := []byte("test-payload")
	svc.dispatchXzyh(2, payload)

	mu.Lock()
	defer mu.Unlock()
	if string(received) != "test-payload" {
		t.Fatalf("received = %q, want %q", received, "test-payload")
	}
}

func TestPPPPService_XzyhHandlerIsolation(t *testing.T) {
	// Handlers registered on channel 0 must not fire for channel 1 dispatches.
	fake := newFakePPPPConn()
	svc := newTestPPPPService(fake)

	called := false
	svc.RegisterXzyhHandler(0, func(data []byte) {
		called = true
	})

	svc.dispatchXzyh(1, []byte("wrong-channel"))
	if called {
		t.Fatal("handler on channel 0 was called for channel 1 dispatch")
	}
}

func TestPPPPService_NilHandlerRegistration(t *testing.T) {
	// RegisterXzyhHandler, RegisterVideoHandler, RegisterAabbHandler must
	// silently ignore nil function arguments.
	fake := newFakePPPPConn()
	svc := newTestPPPPService(fake)

	svc.RegisterXzyhHandler(0, nil)
	svc.RegisterVideoHandler(nil)
	svc.RegisterAabbHandler(0, nil)

	// Dispatching should not panic.
	svc.dispatchXzyh(0, []byte("data"))
	svc.dispatchVideo(protocol.VideoFrame{})
	svc.dispatchAabb(0, protocol.Aabb{}, []byte("data"))
}

func TestPPPPService_AabbHandlerDispatch(t *testing.T) {
	fake := newFakePPPPConn()
	svc := newTestPPPPService(fake)

	var gotAabb protocol.Aabb
	var gotData []byte
	var mu sync.Mutex
	svc.RegisterAabbHandler(1, func(aabb protocol.Aabb, data []byte) {
		mu.Lock()
		gotAabb = aabb
		gotData = append([]byte(nil), data...)
		mu.Unlock()
	})

	testAabb := protocol.Aabb{FrameType: protocol.FileTransferBegin}
	svc.dispatchAabb(1, testAabb, []byte("aabb-payload"))

	mu.Lock()
	defer mu.Unlock()
	if gotAabb.FrameType != protocol.FileTransferBegin {
		t.Fatalf("aabb frame type = %v, want FileTransferBegin", gotAabb.FrameType)
	}
	if string(gotData) != "aabb-payload" {
		t.Fatalf("aabb data = %q, want %q", gotData, "aabb-payload")
	}
}

func TestPPPPService_WorkerStopCleansClient(t *testing.T) {
	// After WorkerStop, currentClient() must return nil and Close must have
	// been called on the previous client.
	fake := newFakePPPPConn()
	svc := newTestPPPPService(fake)
	if err := svc.WorkerStart(); err != nil {
		t.Fatalf("WorkerStart: %v", err)
	}

	if svc.currentClient() == nil {
		t.Fatal("expected non-nil client after WorkerStart")
	}

	svc.WorkerStop()

	if svc.currentClient() != nil {
		t.Fatal("expected nil client after WorkerStop")
	}
	if !fake.closed.Load() {
		t.Fatal("expected Close() to have been called on the client")
	}
}

func TestPPPPService_ConnectionResetDuringRun(t *testing.T) {
	// Multiple sequential connection resets should all result in restart
	// signals. This verifies stale-state recovery — each WorkerRun invocation
	// must detect the reset and signal restart.
	for i := 0; i < 3; i++ {
		fake := newFakePPPPConn()
		fake.runErr = errors.New("connection reset by peer")

		svc := newTestPPPPService(fake)
		if err := svc.WorkerStart(); err != nil {
			t.Fatalf("iteration %d: WorkerStart: %v", i, err)
		}

		err := svc.WorkerRun(context.Background())
		if !IsServiceRestartSignal(err) {
			t.Fatalf("iteration %d: WorkerRun err = %v, want ServiceRestartSignal", i, err)
		}
		svc.WorkerStop()
	}
}

func TestPPPPService_ContextCancellationDuringRun(t *testing.T) {
	// When the context is cancelled during WorkerRun, it should exit cleanly
	// without returning a restart signal.
	fake := newFakePPPPConn()
	fake.runDelay = 30 * time.Second // keep Run alive

	svc := newTestPPPPService(fake)
	if err := svc.WorkerStart(); err != nil {
		t.Fatalf("WorkerStart: %v", err)
	}
	defer svc.WorkerStop()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- svc.WorkerRun(ctx)
	}()

	// Give WorkerRun time to enter the poll loop.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WorkerRun after cancel returned err = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WorkerRun did not exit after context cancellation")
	}
}

func TestProbePPPPWithNilFactory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if probePPPPWithFactory(ctx, nil) {
		t.Fatal("expected false for nil factory")
	}
}

func TestPPPPService_WaitConnectedTimeout(t *testing.T) {
	// waitConnected must return an error if the client never reaches
	// StateConnected within 10 seconds. We use a shorter context deadline
	// to avoid waiting the full 10 seconds.
	fake := newFakePPPPConn()
	fake.state = ppppclient.StateConnecting

	svc := newTestPPPPService(fake)
	svc.client = fake

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := svc.waitConnected(ctx)
	if err == nil {
		t.Fatal("expected error from waitConnected, got nil")
	}
}

func TestPPPPService_WaitConnectedSuccess(t *testing.T) {
	fake := newFakePPPPConn()
	fake.state = ppppclient.StateConnected

	svc := newTestPPPPService(fake)
	svc.client = fake

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cli, err := svc.waitConnected(ctx)
	if err != nil {
		t.Fatalf("waitConnected: %v", err)
	}
	if cli == nil {
		t.Fatal("expected non-nil client from waitConnected")
	}
}
