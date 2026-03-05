package notifications

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
