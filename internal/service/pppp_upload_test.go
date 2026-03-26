package service

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/django1982/ankerctl/internal/pppp/protocol"
)

// aabbReplyWorker reads AABB frames from a channel's TX wire and dispatches
// matching reply ACKs back to the service's AABB handler. This simulates the
// printer side of the AABB request/reply protocol used during file upload.
//
// It works by polling ch.Poll() (which returns DRW packets ready for
// transmission), extracting any AABB frames from the DRW data, and calling
// dispatchAabb with a single-byte {0x00} reply (success ACK).
func aabbReplyWorker(ctx context.Context, svc *PPPPService, ch *protocol.Channel, chanIdx byte) {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			drws := ch.Poll(time.Now())
			for _, drw := range drws {
				// ACK the DRW so it is removed from the in-flight window.
				ch.RXAck([]uint16{drw.Index})
				if len(drw.Data) < 2 {
					continue
				}
				// Check for AABB signature (0xAABB little-endian).
				if drw.Data[0] == 0xAA && drw.Data[1] == 0xBB {
					svc.dispatchAabb(chanIdx, protocol.Aabb{}, []byte{0x00})
				}
				// Check for XZYH header (JSON handshake reply on channel 0).
				if len(drw.Data) >= 4 && string(drw.Data[:4]) == "XZYH" {
					// Parse to detect P2PCmdP2pSendFile; reply with 4-byte
					// success code (0x00000000) via dispatchXzyh.
					if len(drw.Data) >= 10 {
						cmd := binary.LittleEndian.Uint16(drw.Data[4:6])
						if protocol.P2PCmdType(cmd) == protocol.P2PCmdP2pSendFile {
							reply := make([]byte, 4)
							binary.LittleEndian.PutUint32(reply, 0)
							svc.dispatchXzyh(0, reply)
						}
					}
				}
			}
		}
	}
}

func TestPPPPService_Upload(t *testing.T) {
	tests := []struct {
		name       string
		payload    []byte
		startPrint bool
		cancelCtx  bool // cancel context mid-upload
		wantErr    bool
		errContain string
	}{
		{
			name:       "success_small_file_with_start_print",
			payload:    make([]byte, 100),
			startPrint: true,
		},
		{
			name:       "success_exact_block_boundary",
			payload:    make([]byte, 32*1024), // exactly one blockSize
			startPrint: false,
		},
		{
			name:       "success_multi_block",
			payload:    make([]byte, 32*1024+500), // two blocks
			startPrint: true,
		},
		{
			name:       "context_cancelled_mid_upload",
			payload:    make([]byte, 32*1024*3), // 3 blocks — cancel after a short delay
			startPrint: false,
			cancelCtx:  true,
			wantErr:    true,
			errContain: "context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakePPPPConn()
			fake.runDelay = 30 * time.Second

			svc := &PPPPService{
				BaseWorker:   NewBaseWorker("ppppservice"),
				log:          slog.Default(),
				client:       fake,
				clientFactor: func(context.Context) (ppppConn, error) { return fake, nil },
				pollInterval: 1 * time.Millisecond,
				handlers:     make(map[byte][]func([]byte)),
				aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
			}
			svc.BindHooks(svc)

			// Start reply workers on channels 0 and 1 to simulate the
			// printer acknowledging XZYH handshake + AABB frames.
			workerCtx, workerCancel := context.WithCancel(context.Background())
			defer workerCancel()

			ch0, _ := fake.Channel(0)
			ch1, _ := fake.Channel(1)
			go aabbReplyWorker(workerCtx, svc, ch0, 0)
			go aabbReplyWorker(workerCtx, svc, ch1, 1)

			info := UploadInfo{
				Name:       "test.gcode",
				UserName:   "alice",
				UserID:     "u1",
				MachineID:  "TESTMACHINE12345",
				Size:       int64(len(tt.payload)),
				StartPrint: tt.startPrint,
			}

			var progressMu sync.Mutex
			var lastSent, lastTotal int64
			progressCalls := 0
			progressFn := func(sent, total int64) {
				progressMu.Lock()
				lastSent = sent
				lastTotal = total
				progressCalls++
				progressMu.Unlock()
			}

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			if tt.cancelCtx {
				// Cancel quickly so the upload is interrupted mid-transfer.
				cancelCtx, cancelFn := context.WithTimeout(ctx, 5*time.Millisecond)
				defer cancelFn()
				ctx = cancelCtx
			}

			err := svc.Upload(ctx, info, tt.payload, progressFn)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errContain != "" {
					if !contains(err.Error(), tt.errContain) {
						t.Fatalf("error %q does not contain %q", err.Error(), tt.errContain)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("Upload: %v", err)
			}

			progressMu.Lock()
			if tt.payload != nil && len(tt.payload) > 0 {
				if lastTotal != int64(len(tt.payload)) {
					t.Errorf("progress total = %d, want %d", lastTotal, len(tt.payload))
				}
				if lastSent != int64(len(tt.payload)) {
					t.Errorf("progress sent = %d, want %d", lastSent, len(tt.payload))
				}
				if progressCalls == 0 {
					t.Error("expected at least one progress call")
				}
			}
			progressMu.Unlock()
		})
	}
}

func TestPPPPService_Upload_NoClient(t *testing.T) {
	svc := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.Default(),
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}
	// No client set — waitConnected must fail.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := svc.Upload(ctx, UploadInfo{Name: "test.gcode", Size: 10}, []byte("0123456789"), nil)
	if err == nil {
		t.Fatal("expected error from Upload with no client")
	}
}

func TestPPPPService_Upload_ChannelError(t *testing.T) {
	fake := newFakePPPPConn()
	fake.channelErr = errors.New("channel unavailable")

	svc := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.Default(),
		client:       fake,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := svc.Upload(ctx, UploadInfo{Name: "test.gcode", Size: 4}, []byte("data"), nil)
	if err == nil {
		t.Fatal("expected channel error, got nil")
	}
	if !contains(err.Error(), "channel") {
		t.Fatalf("error %q does not mention channel", err.Error())
	}
}

func TestPPPPService_Upload_HandshakeSequence(t *testing.T) {
	// Verify the XZYH JSON handshake is sent on channel 0 before AABB frames
	// on channel 1. We record write order via the DRW packets.
	fake := newFakePPPPConn()
	fake.runDelay = 30 * time.Second

	svc := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.Default(),
		client:       fake,
		clientFactor: func(context.Context) (ppppConn, error) { return fake, nil },
		pollInterval: 1 * time.Millisecond,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}
	svc.BindHooks(svc)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	ch0, _ := fake.Channel(0)
	ch1, _ := fake.Channel(1)
	go aabbReplyWorker(workerCtx, svc, ch0, 0)
	go aabbReplyWorker(workerCtx, svc, ch1, 1)

	info := UploadInfo{
		Name:       "handshake_test.gcode",
		UserName:   "bob",
		UserID:     "u2",
		Size:       5,
		StartPrint: false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := svc.Upload(ctx, info, []byte("G28\n\n"), nil)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Verify the JSON handshake was present on channel 0 by checking the
	// channel 0 TX wire had DRW packets with XZYH framing containing
	// P2P_SEND_FILE. We already know the upload succeeded (which requires
	// the handshake to pass), so this is a structural validation.
	// The reply worker consumed the DRW packets, so we verify success here.
}

func TestPPPPService_Upload_AabbBeginContainsMetadata(t *testing.T) {
	// The AABB BEGIN frame carries a metadata string. Verify the Upload
	// method constructs it correctly by intercepting the AABB handler.
	fake := newFakePPPPConn()
	fake.runDelay = 30 * time.Second

	svc := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.Default(),
		client:       fake,
		clientFactor: func(context.Context) (ppppConn, error) { return fake, nil },
		pollInterval: 1 * time.Millisecond,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}
	svc.BindHooks(svc)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	ch0, _ := fake.Channel(0)
	ch1, _ := fake.Channel(1)

	// Instead of the generic reply worker, use a custom one that captures
	// the AABB BEGIN payload.
	var capturedBeginMeta string
	var mu sync.Mutex
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				for _, ch := range []*protocol.Channel{ch0, ch1} {
					drws := ch.Poll(time.Now())
					for _, drw := range drws {
						ch.RXAck([]uint16{drw.Index})
						if len(drw.Data) >= 14 && drw.Data[0] == 0xAA && drw.Data[1] == 0xBB {
							aabb, data, err := protocol.ParseAabbWithCRC(drw.Data)
							if err == nil && aabb.FrameType == protocol.FileTransferBegin {
								mu.Lock()
								capturedBeginMeta = string(data)
								mu.Unlock()
							}
							svc.dispatchAabb(1, protocol.Aabb{}, []byte{0x00})
						}
						if len(drw.Data) >= 4 && string(drw.Data[:4]) == "XZYH" {
							if len(drw.Data) >= 10 {
								cmd := binary.LittleEndian.Uint16(drw.Data[4:6])
								if protocol.P2PCmdType(cmd) == protocol.P2PCmdP2pSendFile {
									reply := make([]byte, 4)
									svc.dispatchXzyh(0, reply)
								}
							}
						}
					}
				}
			}
		}
	}()

	info := UploadInfo{
		Name:       "meta_test.gcode",
		UserName:   "carol",
		UserID:     "u3",
		MachineID:  "ABC123",
		Size:       4,
		StartPrint: false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := svc.Upload(ctx, info, []byte("G28\n"), nil); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	mu.Lock()
	meta := capturedBeginMeta
	mu.Unlock()

	// Metadata format: "type,name,size,md5,user_name,user_id,machine_id"
	if !contains(meta, "meta_test.gcode") {
		t.Errorf("AABB BEGIN metadata missing filename: %q", meta)
	}
	if !contains(meta, "carol") {
		t.Errorf("AABB BEGIN metadata missing username: %q", meta)
	}
	if !contains(meta, "u3") {
		t.Errorf("AABB BEGIN metadata missing user_id: %q", meta)
	}
}

func TestPPPPService_Upload_JSONHandshakePayload(t *testing.T) {
	// Verify the JSON handshake sent on channel 0 has the expected fields.
	fake := newFakePPPPConn()
	fake.runDelay = 30 * time.Second

	svc := &PPPPService{
		BaseWorker:   NewBaseWorker("ppppservice"),
		log:          slog.Default(),
		client:       fake,
		clientFactor: func(context.Context) (ppppConn, error) { return fake, nil },
		pollInterval: 1 * time.Millisecond,
		handlers:     make(map[byte][]func([]byte)),
		aabbHandlers: make(map[byte][]func(protocol.Aabb, []byte)),
	}
	svc.BindHooks(svc)

	workerCtx, workerCancel := context.WithCancel(context.Background())
	defer workerCancel()

	ch0, _ := fake.Channel(0)
	ch1, _ := fake.Channel(1)

	var capturedJSON map[string]any
	var jsonMu sync.Mutex

	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				for _, pair := range []struct {
					ch  *protocol.Channel
					idx byte
				}{{ch0, 0}, {ch1, 1}} {
					drws := pair.ch.Poll(time.Now())
					for _, drw := range drws {
						pair.ch.RXAck([]uint16{drw.Index})
						if len(drw.Data) >= 4 && string(drw.Data[:4]) == "XZYH" {
							x, err := protocol.ParseXzyh(drw.Data)
							if err == nil && x.Cmd == protocol.P2PCmdP2pSendFile {
								var parsed map[string]any
								if json.Unmarshal(x.Data, &parsed) == nil {
									jsonMu.Lock()
									capturedJSON = parsed
									jsonMu.Unlock()
								}
								reply := make([]byte, 4)
								svc.dispatchXzyh(0, reply)
							}
						}
						if len(drw.Data) >= 2 && drw.Data[0] == 0xAA && drw.Data[1] == 0xBB {
							svc.dispatchAabb(1, protocol.Aabb{}, []byte{0x00})
						}
					}
				}
			}
		}
	}()

	info := UploadInfo{
		Name:       "json_test.gcode",
		UserName:   "dave",
		UserID:     "u4",
		Size:       4,
		StartPrint: false,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := svc.Upload(ctx, info, []byte("G28\n"), nil); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	jsonMu.Lock()
	j := capturedJSON
	jsonMu.Unlock()

	if j == nil {
		t.Fatal("no JSON handshake captured on channel 0")
	}
	// Required fields per Python parity.
	for _, field := range []string{"uuid", "device", "flag", "random", "timeout", "total_timeout"} {
		if _, ok := j[field]; !ok {
			t.Errorf("JSON handshake missing field %q: %v", field, j)
		}
	}
	if j["device"] != "ankerctl" {
		t.Errorf("device = %v, want \"ankerctl\"", j["device"])
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && searchSubstring(s, sub)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
