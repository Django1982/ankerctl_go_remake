package service

import (
	"context"
	"errors"
	"log/slog"
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
func (f *fakePPPPConn) Close() error { return nil }
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
