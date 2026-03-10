package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	ppppclient "github.com/django1982/ankerctl/internal/pppp/client"
	"github.com/django1982/ankerctl/internal/pppp/protocol"
)

type fakePPPPConn struct {
	runErr error
	state  ppppclient.State
	chans  [8]*protocol.Channel
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
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(5 * time.Millisecond):
		return f.runErr
	}
}
func (f *fakePPPPConn) Close() error  { return nil }
func (f *fakePPPPConn) RemoteIP() net.IP { return nil }
func (f *fakePPPPConn) State() ppppclient.State {
	return f.state
}
func (f *fakePPPPConn) Channel(index int) (*protocol.Channel, error) {
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
