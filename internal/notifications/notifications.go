package notifications

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/django1982/ankerctl/internal/model"
)

const (
	mqttStateIdle     = 0
	mqttStatePrinting = 1
	mqttStatePaused   = 2
	mqttStateAborted  = 8
)

var newClient = NewClient

// ConfigLoader loads runtime config.
type ConfigLoader interface {
	Load() (*model.Config, error)
}

// EventTapSource exposes Tap subscription behavior.
type EventTapSource interface {
	Tap(handler func(any)) func()
}

// SnapshotCapturer can capture a JPEG snapshot to disk.
type SnapshotCapturer interface {
	CaptureSnapshot(ctx context.Context, outputPath string) error
}

// NotificationService wires MQTT events to Apprise notifications.
type NotificationService struct {
	cfg      ConfigLoader
	mqtt     EventTapSource
	snapshot SnapshotCapturer

	mu                   sync.Mutex
	lastState            int
	lastFilename         string
	lastProgress         int
	lastProgressNotified int // last progress % that triggered a notification
	progressSendCount    int // how many progress notifications sent this print
}

// NewNotificationService creates a notification bridge.
func NewNotificationService(cfg ConfigLoader, mqtt EventTapSource, snapshot SnapshotCapturer) *NotificationService {
	return &NotificationService{
		cfg:                  cfg,
		mqtt:                 mqtt,
		snapshot:             snapshot,
		lastState:            -1,
		lastFilename:         "-",
		lastProgressNotified: -1,
	}
}

// Start subscribes to MQTT events and sends notifications until ctx is cancelled.
func (s *NotificationService) Start(ctx context.Context) error {
	if s.mqtt == nil {
		return fmt.Errorf("notifications: mqtt source is nil")
	}
	events := make(chan any, 128)
	unsub := s.mqtt.Tap(func(v any) {
		select {
		case events <- v:
		default:
		}
	})
	defer unsub()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case evt := <-events:
			s.handleEvent(ctx, evt)
		}
	}
}

// SendTestNotification sends a plain test message using a config snapshot.
func SendTestNotification(ctx context.Context, apprise model.AppriseConfig, snapshot SnapshotCapturer) (bool, string) {
	resolved := ResolveAppriseEnv(apprise)
	client := newClient(resolved)
	if !client.IsConfigured() {
		return false, "Apprise server URL or key missing"
	}
	attachments := maybeSnapshotAttachment(ctx, snapshot)
	return client.Post(ctx, "Ankerctl Test", "Test notification sent from ankerctl settings page.", "info", attachments)
}

func (s *NotificationService) handleEvent(ctx context.Context, evt any) {
	payload, ok := evt.(map[string]any)
	if !ok {
		return
	}

	if fn := extractFilename(payload); fn != "" {
		s.mu.Lock()
		s.lastFilename = fn
		s.mu.Unlock()
	}
	if p, ok := extractProgress(payload); ok {
		s.mu.Lock()
		s.lastProgress = p
		s.mu.Unlock()
		// Check if we should fire a progress notification.
		s.maybeProgressNotification(ctx, p)
	}

	eventName, _ := payload["event"].(string)
	if eventName != "print_state" {
		return
	}
	state, ok := asInt(payload["state"])
	if !ok {
		return
	}
	s.handleStateTransition(ctx, state)
}

func (s *NotificationService) maybeProgressNotification(ctx context.Context, progress int) {
	client := s.currentClient()
	if client == nil || !client.IsEventEnabled(EventPrintProgress) {
		return
	}

	interval := client.settings.Progress.IntervalPercent
	if interval < 1 {
		interval = 1
	}
	if interval > 100 {
		interval = 100
	}

	s.mu.Lock()
	lastNotified := s.lastProgressNotified
	state := s.lastState
	filename := s.lastFilename
	sendCount := s.progressSendCount
	maxValue := client.settings.Progress.MaxValue
	s.mu.Unlock()

	// Only send during an active print.
	if state != mqttStatePrinting {
		return
	}

	// Determine if we crossed an interval threshold.
	nextThreshold := lastNotified + interval
	if lastNotified < 0 {
		nextThreshold = interval
	}
	if progress < nextThreshold {
		return
	}

	// Check max_value cap.
	if maxValue > 0 && sendCount >= maxValue {
		return
	}

	s.mu.Lock()
	s.lastProgressNotified = progress
	s.progressSendCount++
	s.mu.Unlock()

	payload := map[string]any{
		"filename": filename,
		"percent":  progress,
	}
	s.send(ctx, EventPrintProgress, payload)
}

func (s *NotificationService) handleStateTransition(ctx context.Context, state int) {
	s.mu.Lock()
	prev := s.lastState
	s.lastState = state
	filename := s.lastFilename
	progress := s.lastProgress
	s.mu.Unlock()

	payload := map[string]any{
		"filename": filename,
		"percent":  progress,
		"duration": "",
		"reason":   "",
	}

	switch state {
	case mqttStatePrinting:
		if prev == mqttStatePaused {
			s.send(ctx, EventPrintResumed, payload)
			return
		}
		if prev != mqttStatePrinting {
			// Reset progress tracking for new print.
			s.mu.Lock()
			s.lastProgressNotified = -1
			s.progressSendCount = 0
			s.mu.Unlock()
			s.send(ctx, EventPrintStarted, payload)
		}
	case mqttStatePaused:
		if prev == mqttStatePrinting {
			s.send(ctx, EventPrintPaused, payload)
		}
	case mqttStateIdle:
		if prev == mqttStatePrinting || prev == mqttStatePaused {
			// Reset progress tracking.
			s.mu.Lock()
			s.lastProgressNotified = -1
			s.progressSendCount = 0
			s.mu.Unlock()
			s.send(ctx, EventPrintFinished, payload)
		}
	case mqttStateAborted:
		if prev == mqttStatePrinting || prev == mqttStatePaused {
			payload["reason"] = "aborted"
			// Reset progress tracking.
			s.mu.Lock()
			s.lastProgressNotified = -1
			s.progressSendCount = 0
			s.mu.Unlock()
			s.send(ctx, EventPrintFailed, payload)
		}
	}
}

func (s *NotificationService) send(ctx context.Context, event string, payload map[string]any) {
	client := s.currentClient()
	if client == nil {
		return
	}
	attachments := maybeSnapshotAttachment(ctx, s.snapshot)
	_, _ = client.SendEvent(ctx, event, payload, attachments)
}

func (s *NotificationService) currentClient() *Client {
	if s.cfg == nil {
		return nil
	}
	cfg, err := s.cfg.Load()
	if err != nil || cfg == nil {
		return nil
	}
	resolved := ResolveAppriseEnv(cfg.Notifications.Apprise)
	return newClient(resolved)
}

func maybeSnapshotAttachment(ctx context.Context, snapshot SnapshotCapturer) []string {
	if !envBool("APPRISE_ATTACH") || snapshot == nil {
		return nil
	}
	tmp, err := os.CreateTemp("", "apprise-*.jpg")
	if err != nil {
		return nil
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)

	snapCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if err := snapshot.CaptureSnapshot(snapCtx, tmpPath); err != nil {
		return nil
	}
	data, err := os.ReadFile(tmpPath)
	if err != nil || len(data) == 0 {
		return nil
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return []string{"data:image/jpeg;base64," + encoded}
}

func envBool(key string) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return false
	}
	v, err := strconv.ParseBool(raw)
	if err == nil {
		return v
	}
	switch raw {
	case "yes", "y", "on", "t", "1":
		return true
	case "no", "n", "off", "f", "0":
		return false
	default:
		return false
	}
}

func extractFilename(payload map[string]any) string {
	for _, key := range []string{"name", "fileName", "filename", "file_name", "gcode", "gcode_name", "filePath"} {
		v, ok := payload[key].(string)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		if key == "filePath" {
			return filepath.Base(v)
		}
		return strings.TrimSpace(v)
	}
	return ""
}

func extractProgress(payload map[string]any) (int, bool) {
	if v, ok := asInt(payload["progress"]); ok {
		if v < 0 {
			return 0, true
		}
		if v > 100 {
			if v <= 10000 {
				return v / 100, true
			}
			return 100, true
		}
		return v, true
	}
	return 0, false
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int8:
		return int(x), true
	case int16:
		return int(x), true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case uint:
		return int(x), true
	case uint8:
		return int(x), true
	case uint16:
		return int(x), true
	case uint32:
		return int(x), true
	case uint64:
		return int(x), true
	case float32:
		return int(x), true
	case float64:
		return int(x), true
	default:
		return 0, false
	}
}

