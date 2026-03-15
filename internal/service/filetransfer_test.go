package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockUploader struct {
	mu       sync.Mutex
	calls    int
	lastInfo UploadInfo
	lastData []byte
	err      error
	delay    time.Duration
}

func (m *mockUploader) Upload(ctx context.Context, info UploadInfo, payload []byte, progress func(sent, total int64)) error {
	m.mu.Lock()
	m.calls++
	m.lastInfo = info
	m.lastData = append([]byte(nil), payload...)
	uploadErr := m.err
	delay := m.delay
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	if uploadErr != nil {
		return uploadErr
	}

	total := int64(len(payload))
	progress(total/2, total)
	progress(total, total)
	return nil
}

type mockLayerReceiver struct {
	mu    sync.Mutex
	layer int
}

func (m *mockLayerReceiver) SetGCodeLayerCount(layerCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.layer = layerCount
}

func TestFileTransferServiceSendFile(t *testing.T) {
	uploader := &mockUploader{}
	mqtt := &mockLayerReceiver{}
	svc := NewFileTransferService(uploader, mqtt)
	defer svc.Shutdown()
	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)

	data := []byte("; estimated printing time = 1h 2m 3s\n;LAYER_COUNT:123\nG28\nG1 X0 Y0\n")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	events := make([]FileTransferEvent, 0)
	unsub := svc.Tap(func(v any) {
		evt, ok := v.(FileTransferEvent)
		if ok {
			events = append(events, evt)
		}
	})
	defer unsub()

	if err := svc.SendFile(ctx, "demo.gcode", "alice", "user-1", data, 10, true); err != nil {
		t.Fatalf("SendFile: %v", err)
	}

	uploader.mu.Lock()
	calls := uploader.calls
	patched := string(uploader.lastData)
	info := uploader.lastInfo
	uploader.mu.Unlock()
	if calls != 1 {
		t.Fatalf("upload calls = %d, want 1", calls)
	}
	if !strings.Contains(patched, ";TIME:3723") {
		t.Fatalf("patched gcode missing ;TIME marker: %q", patched)
	}
	if info.LayerCount != 123 {
		t.Fatalf("layer count = %d, want 123", info.LayerCount)
	}

	mqtt.mu.Lock()
	layer := mqtt.layer
	mqtt.mu.Unlock()
	if layer != 123 {
		t.Fatalf("mqtt layer = %d, want 123", layer)
	}

	if len(events) < 2 {
		t.Fatalf("expected progress events, got %d", len(events))
	}
	if events[len(events)-1].Status != "done" {
		t.Fatalf("last event = %+v, want done", events[len(events)-1])
	}
}

func TestFileTransferService_UploadError(t *testing.T) {
	// Simulate a PPPP channel error during upload — the uploader returns an
	// error, which must propagate back through SendFile and emit an "error"
	// event notification.
	uploader := &mockUploader{err: errors.New("pppp channel broken")}
	svc := NewFileTransferService(uploader, nil)
	defer svc.Shutdown()
	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)

	var events []FileTransferEvent
	var mu sync.Mutex
	unsub := svc.Tap(func(v any) {
		if evt, ok := v.(FileTransferEvent); ok {
			mu.Lock()
			events = append(events, evt)
			mu.Unlock()
		}
	})
	defer unsub()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := svc.SendFile(ctx, "fail.gcode", "bob", "u2", []byte("G28\n"), 0, false)
	if err == nil {
		t.Fatal("expected error from SendFile, got nil")
	}
	if !strings.Contains(err.Error(), "upload failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	// The service must have emitted an "error" event.
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, evt := range events {
		if evt.Status == "error" && strings.Contains(evt.Err, "pppp channel broken") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected error event with channel message, got events: %+v", events)
	}
}

func TestFileTransferService_ContextCancellation(t *testing.T) {
	// Simulate an upload that blocks long enough for the caller's context to
	// expire. SendFile must return the context error rather than blocking
	// indefinitely.
	uploader := &mockUploader{delay: 5 * time.Second}
	svc := NewFileTransferService(uploader, nil)
	defer svc.Shutdown()
	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := svc.SendFile(ctx, "cancel.gcode", "carol", "u3", []byte("G28\n"), 0, false)
	if err == nil {
		t.Fatal("expected context deadline error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context error, got: %v", err)
	}
}

func TestFileTransferService_EmptyGCode(t *testing.T) {
	// An empty GCode file should still be uploadable — the patching step must
	// not panic, and the uploader receives the (possibly untouched) payload.
	uploader := &mockUploader{}
	mqtt := &mockLayerReceiver{}
	svc := NewFileTransferService(uploader, mqtt)
	defer svc.Shutdown()
	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := svc.SendFile(ctx, "empty.gcode", "dave", "u4", []byte{}, 0, false); err != nil {
		t.Fatalf("SendFile with empty data: %v", err)
	}

	uploader.mu.Lock()
	calls := uploader.calls
	uploader.mu.Unlock()
	if calls != 1 {
		t.Fatalf("upload calls = %d, want 1", calls)
	}

	// Layer count should not have been forwarded for empty data.
	mqtt.mu.Lock()
	layer := mqtt.layer
	mqtt.mu.Unlock()
	if layer != 0 {
		t.Fatalf("expected layer=0 for empty gcode, got %d", layer)
	}
}

func TestFileTransferService_NilUploader(t *testing.T) {
	// WorkerStart must fail when uploader is nil.
	svc := NewFileTransferService(nil, nil)
	defer svc.Shutdown()

	err := svc.WorkerStart()
	if err == nil {
		t.Fatal("expected error from WorkerStart with nil uploader")
	}
	if !strings.Contains(err.Error(), "uploader not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestFileTransferService_SendFileBeforeStart(t *testing.T) {
	// If SendFile is called with a context that expires before the reqCh is
	// drained (because the service is not running), the context error should
	// be returned.
	uploader := &mockUploader{}
	svc := NewFileTransferService(uploader, nil)
	// Intentionally NOT starting the service.

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := svc.SendFile(ctx, "never.gcode", "eve", "u5", []byte("G28\n"), 0, false)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestFileTransferService_EventSequence(t *testing.T) {
	// Verify the complete event lifecycle: start → progress → done.
	uploader := &mockUploader{}
	svc := NewFileTransferService(uploader, nil)
	defer svc.Shutdown()
	svc.Start(context.Background())
	waitForState(t, svc, StateRunning, 2*time.Second)

	var events []FileTransferEvent
	var mu sync.Mutex
	unsub := svc.Tap(func(v any) {
		if evt, ok := v.(FileTransferEvent); ok {
			mu.Lock()
			events = append(events, evt)
			mu.Unlock()
		}
	})
	defer unsub()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := svc.SendFile(ctx, "seq.gcode", "frank", "u6", []byte("G28\n"), 0, false); err != nil {
		t.Fatalf("SendFile: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (start+done), got %d: %+v", len(events), events)
	}
	if events[0].Status != "start" {
		t.Fatalf("first event status = %q, want \"start\"", events[0].Status)
	}
	if events[0].Name != "seq.gcode" {
		t.Fatalf("first event name = %q, want \"seq.gcode\"", events[0].Name)
	}
	last := events[len(events)-1]
	if last.Status != "done" {
		t.Fatalf("last event status = %q, want \"done\"", last.Status)
	}
	if last.Percentage != 100 {
		t.Fatalf("last event percentage = %d, want 100", last.Percentage)
	}
}
