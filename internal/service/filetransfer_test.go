package service

import (
	"context"
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
}

func (m *mockUploader) Upload(ctx context.Context, info UploadInfo, payload []byte, progress func(sent, total int64)) error {
	m.mu.Lock()
	m.calls++
	m.lastInfo = info
	m.lastData = append([]byte(nil), payload...)
	m.mu.Unlock()

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
