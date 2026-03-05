package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type mockVideoController struct {
	mu       sync.Mutex
	start    int
	stop     int
	lastMode int
}

func (m *mockVideoController) StartLive(ctx context.Context, mode int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.start++
	m.lastMode = mode
	return nil
}
func (m *mockVideoController) StopLive(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stop++
	return nil
}
func (m *mockVideoController) SetVideoMode(ctx context.Context, mode int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastMode = mode
	return nil
}

type mockLightController struct {
	mu    sync.Mutex
	calls []bool
}

func (m *mockLightController) SetLight(ctx context.Context, on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, on)
	return nil
}

func TestVideoQueueStallTriggersRestart(t *testing.T) {
	controller := &mockVideoController{}
	q := NewVideoQueue(controller, nil)
	q.stallTimeout = 20 * time.Millisecond
	q.checkInterval = 5 * time.Millisecond
	q.SetVideoEnabled(true)

	if err := q.WorkerStart(); err != nil {
		t.Fatalf("WorkerStart: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := q.WorkerRun(ctx)
	if !errors.Is(err, ErrServiceRestartSignal) {
		t.Fatalf("WorkerRun err = %v, want restart signal", err)
	}
}

func TestVideoQueueCaptureSnapshotTurnsOnLight(t *testing.T) {
	light := &mockLightController{}
	q := NewVideoQueue(nil, light)

	runs := 0
	q.runFFmpeg = func(ctx context.Context, args []string) error {
		runs++
		out := args[len(args)-1]
		return os.WriteFile(out, []byte("jpg"), 0o644)
	}

	dir := t.TempDir()
	out := filepath.Join(dir, "snap.jpg")
	if err := q.CaptureSnapshot(context.Background(), out); err != nil {
		t.Fatalf("CaptureSnapshot: %v", err)
	}

	if runs == 0 {
		t.Fatal("expected ffmpeg runner to be called")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("snapshot file missing: %v", err)
	}
	if len(light.calls) == 0 || !light.calls[0] {
		t.Fatalf("expected light ON command before snapshot, got %v", light.calls)
	}
}

func TestVideoQueueSetProfile(t *testing.T) {
	controller := &mockVideoController{}
	q := NewVideoQueue(controller, nil)
	if err := q.SetProfile("sd"); err != nil {
		t.Fatalf("SetProfile(sd): %v", err)
	}
	controller.mu.Lock()
	mode := controller.lastMode
	controller.mu.Unlock()
	if mode != 0 {
		t.Fatalf("last mode = %d, want 0", mode)
	}
}
