package notifications

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/django1982/ankerctl/internal/model"
)

func testAppriseConfig(serverURL string) model.AppriseConfig {
	cfg := model.DefaultAppriseConfig()
	cfg.Enabled = true
	cfg.ServerURL = serverURL
	cfg.Key = "test-key"
	cfg.Events.PrintStarted = true
	return cfg
}

func TestClientSendEvent_PostsExpectedJSON(t *testing.T) {
	var got map[string]any
	client := NewClient(testAppriseConfig("https://notify.example.com"))
	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://notify.example.com/notify/test-key" {
			t.Fatalf("url = %s", req.URL.String())
		}
		if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return jsonResponse(http.StatusOK, `{"success":true,"message":"ok"}`), nil
	})
	ok, msg := client.SendEvent(context.Background(), EventPrintStarted, map[string]any{"filename": "part.gcode"}, nil)
	if !ok {
		t.Fatalf("send failed: %s", msg)
	}
	if got["title"] != "Print started" {
		t.Fatalf("title = %#v", got["title"])
	}
	if got["body"] != "Print started: part.gcode" {
		t.Fatalf("body = %#v", got["body"])
	}
	if got["type"] != "info" {
		t.Fatalf("type = %#v", got["type"])
	}
}

func TestClientSendEvent_ContextCanceled(t *testing.T) {
	client := NewClient(testAppriseConfig("https://notify.example.com"))
	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, _ := client.SendEvent(ctx, EventPrintStarted, map[string]any{"filename": "part.gcode"}, nil)
	if ok {
		t.Fatal("expected canceled context send to fail")
	}
}

type fakeSnapshot struct{}

func (f fakeSnapshot) CaptureSnapshot(_ context.Context, outputPath string) error {
	return os.WriteFile(outputPath, []byte("jpeg-bytes"), 0o600)
}

func TestSendTestNotification_AttachSnapshotBase64(t *testing.T) {
	t.Setenv("APPRISE_ATTACH", "true")

	var got map[string]any
	cfg := testAppriseConfig("https://notify.example.com")
	client := NewClient(cfg)
	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return jsonResponse(http.StatusOK, `{"success":true}`), nil
	})
	orig := newClient
	newClient = func(settings model.AppriseConfig) *Client { return client }
	defer func() { newClient = orig }()

	ok, msg := SendTestNotification(context.Background(), cfg, fakeSnapshot{})
	if !ok {
		t.Fatalf("SendTestNotification failed: %s", msg)
	}

	attach, ok := got["attach"].([]any)
	if !ok || len(attach) != 1 {
		t.Fatalf("attach payload missing or wrong type: %#v", got["attach"])
	}
	entry, _ := attach[0].(string)
	if !strings.HasPrefix(entry, "data:image/jpeg;base64,") {
		t.Fatalf("attach entry prefix: %q", entry)
	}
	encoded := strings.TrimPrefix(entry, "data:image/jpeg;base64,")
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if string(decoded) != "jpeg-bytes" {
		t.Fatalf("decoded snapshot = %q", string(decoded))
	}
}

func TestExtractFilename_FilePathBasename(t *testing.T) {
	payload := map[string]any{"filePath": filepath.Join("/tmp", "folder", "part.gcode")}
	if got := extractFilename(payload); got != "part.gcode" {
		t.Fatalf("filename = %q", got)
	}
}

func TestClientPost_MultipartFileUpload(t *testing.T) {
	// Create a temp file to act as an attachment.
	tmpFile, err := os.CreateTemp("", "apprise-test-*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	_, _ = tmpFile.WriteString("file-content")
	tmpFile.Close()

	var receivedContentType string
	var receivedTitle, receivedBody, receivedAttachName string
	var receivedAttachContent []byte

	cfg := testAppriseConfig("https://notify.example.com")
	client := NewClient(cfg)
	client.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		receivedContentType = req.Header.Get("Content-Type")
		mediaType, params, err := mime.ParseMediaType(receivedContentType)
		if err != nil {
			t.Fatalf("parse content type: %v", err)
		}
		if mediaType != "multipart/form-data" {
			t.Fatalf("expected multipart/form-data, got %s", mediaType)
		}
		reader := multipart.NewReader(req.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("read part: %v", err)
			}
			data, _ := io.ReadAll(part)
			switch part.FormName() {
			case "title":
				receivedTitle = string(data)
			case "body":
				receivedBody = string(data)
			case "attach1":
				receivedAttachName = part.FileName()
				receivedAttachContent = data
			}
		}
		return jsonResponse(http.StatusOK, `{"success":true,"message":"ok"}`), nil
	})

	ok, msg := client.Post(context.Background(), "Test Title", "Test Body", "info", []string{tmpFile.Name()})
	if !ok {
		t.Fatalf("Post failed: %s", msg)
	}
	if receivedTitle != "Test Title" {
		t.Fatalf("title = %q", receivedTitle)
	}
	if receivedBody != "Test Body" {
		t.Fatalf("body = %q", receivedBody)
	}
	if receivedAttachName != filepath.Base(tmpFile.Name()) {
		t.Fatalf("attach filename = %q", receivedAttachName)
	}
	if string(receivedAttachContent) != "file-content" {
		t.Fatalf("attach content = %q", string(receivedAttachContent))
	}
}

func TestProgressNotification_IntervalTracking(t *testing.T) {
	var mu sync.Mutex
	var sentEvents []string

	cfg := testAppriseConfig("https://notify.example.com")
	cfg.Events.PrintProgress = true
	cfg.Progress.IntervalPercent = 25
	cfg.Progress.MaxValue = 0 // unlimited

	// Mock config loader.
	loader := &mockConfigLoader{cfg: &model.Config{
		Notifications: model.NotificationsConfig{Apprise: cfg},
	}}

	// Mock MQTT tap source.
	tap := &mockTapSource{}

	svc := NewNotificationService(loader, tap, nil)
	svc.lastState = mqttStatePrinting // Simulate active print.

	orig := newClient
	newClient = func(settings model.AppriseConfig) *Client {
		c := NewClient(settings)
		c.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			mu.Lock()
			sentEvents = append(sentEvents, body["title"].(string))
			mu.Unlock()
			return jsonResponse(http.StatusOK, `{"success":true}`), nil
		})
		return c
	}
	defer func() { newClient = orig }()

	ctx := context.Background()

	// Send progress events: should fire at 25, 50, 75, 100.
	for p := 0; p <= 100; p += 5 {
		svc.maybeProgressNotification(ctx, p)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sentEvents) != 4 {
		t.Fatalf("expected 4 progress notifications, got %d: %v", len(sentEvents), sentEvents)
	}
}

func TestProgressNotification_MaxValueCap(t *testing.T) {
	var mu sync.Mutex
	sendCount := 0

	cfg := testAppriseConfig("https://notify.example.com")
	cfg.Events.PrintProgress = true
	cfg.Progress.IntervalPercent = 10
	cfg.Progress.MaxValue = 2 // cap at 2

	loader := &mockConfigLoader{cfg: &model.Config{
		Notifications: model.NotificationsConfig{Apprise: cfg},
	}}
	tap := &mockTapSource{}
	svc := NewNotificationService(loader, tap, nil)
	svc.lastState = mqttStatePrinting

	orig := newClient
	newClient = func(settings model.AppriseConfig) *Client {
		c := NewClient(settings)
		c.http.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			sendCount++
			mu.Unlock()
			return jsonResponse(http.StatusOK, `{"success":true}`), nil
		})
		return c
	}
	defer func() { newClient = orig }()

	ctx := context.Background()
	for p := 0; p <= 100; p += 5 {
		svc.maybeProgressNotification(ctx, p)
	}

	mu.Lock()
	defer mu.Unlock()
	if sendCount != 2 {
		t.Fatalf("expected 2 progress notifications (max_value=2), got %d", sendCount)
	}
}

// Mock helpers

type mockConfigLoader struct {
	cfg *model.Config
}

func (m *mockConfigLoader) Load() (*model.Config, error) {
	return m.cfg, nil
}

type mockTapSource struct{}

func (m *mockTapSource) Tap(handler func(any)) func() {
	return func() {}
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

