package service

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestServiceInterfaceImplementedByBaseWorker(t *testing.T) {
	var _ Service = (*BaseWorker)(nil)
}

func TestIsServiceRestartSignal(t *testing.T) {
	if !IsServiceRestartSignal(ErrServiceRestartSignal) {
		t.Fatal("expected ErrServiceRestartSignal to match")
	}
	if !IsServiceRestartSignal(fmt.Errorf("wrapped: %w", ErrServiceRestartSignal)) {
		t.Fatal("expected wrapped restart signal to match")
	}
	if IsServiceRestartSignal(nil) {
		t.Fatal("expected nil error to not match")
	}
}

func TestWorkerLifecycleStateMachine(t *testing.T) {
	w := newTestWorker("lifecycle-worker")
	w.restartOn = -1
	defer w.Shutdown()

	w.Start(context.Background())

	waitForState(t, w, StateRunning, 2*time.Second)
	w.Stop()
	waitForState(t, w, StateStopped, 2*time.Second)
}

func waitForState(t *testing.T, svc Service, expected RunState, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if svc.State() == expected {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for state %v (got %v)", expected, svc.State())
}
