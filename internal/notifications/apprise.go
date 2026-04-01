package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/django1982/ankerctl/internal/model"
)

const defaultTimeout = 10 * time.Second

// eventEnvOverrides maps event names to their env-var overrides.
var eventEnvOverrides = map[string]string{
	EventPrintStarted:  "APPRISE_EVENT_PRINT_STARTED",
	EventPrintFinished: "APPRISE_EVENT_PRINT_FINISHED",
	EventPrintFailed:   "APPRISE_EVENT_PRINT_FAILED",
	EventGCodeUploaded: "APPRISE_EVENT_GCODE_UPLOADED",
	EventPrintProgress: "APPRISE_EVENT_PRINT_PROGRESS",
}

// Client sends notifications to an Apprise API server.
type Client struct {
	settings model.AppriseConfig
	http     *http.Client
}

// NewClient builds an Apprise client with a 10-second HTTP timeout.
func NewClient(settings model.AppriseConfig) *Client {
	return &Client{
		settings: settings,
		http: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// ResolveAppriseEnv applies APPRISE_* environment variable overrides to a config,
// matching Python's AppriseClient._resolve_settings().
func ResolveAppriseEnv(cfg model.AppriseConfig) model.AppriseConfig {
	if v, ok := readBoolEnv("APPRISE_ENABLED"); ok {
		cfg.Enabled = v
	}
	if v := os.Getenv("APPRISE_SERVER_URL"); v != "" {
		cfg.ServerURL = v
	}
	if v := os.Getenv("APPRISE_KEY"); v != "" {
		cfg.Key = v
	}
	if v := os.Getenv("APPRISE_TAG"); v != "" {
		cfg.Tag = v
	}

	// Event overrides
	for event, envVar := range eventEnvOverrides {
		if v, ok := readBoolEnv(envVar); ok {
			switch event {
			case EventPrintStarted:
				cfg.Events.PrintStarted = v
			case EventPrintFinished:
				cfg.Events.PrintFinished = v
			case EventPrintFailed:
				cfg.Events.PrintFailed = v
			case EventGCodeUploaded:
				cfg.Events.GcodeUploaded = v
			case EventPrintProgress:
				cfg.Events.PrintProgress = v
			}
		}
	}

	// Progress overrides
	if v, ok := readIntEnv("APPRISE_PROGRESS_INTERVAL"); ok {
		cfg.Progress.IntervalPercent = v
	}
	if v, ok := readBoolEnv("APPRISE_PROGRESS_INCLUDE_IMAGE"); ok {
		cfg.Progress.IncludeImage = v
	}
	if v := os.Getenv("APPRISE_SNAPSHOT_QUALITY"); v != "" {
		cfg.Progress.SnapshotQuality = v
	}
	if v, ok := readBoolEnv("APPRISE_SNAPSHOT_FALLBACK"); ok {
		cfg.Progress.SnapshotFallback = v
	}
	if v, ok := readBoolEnv("APPRISE_SNAPSHOT_LIGHT"); ok {
		cfg.Progress.SnapshotLight = v
	}
	if v, ok := readIntEnv("APPRISE_PROGRESS_MAX"); ok {
		cfg.Progress.MaxValue = v
	}

	return cfg
}

// readBoolEnv reads an env var as bool, returning (value, found).
func readBoolEnv(key string) (bool, bool) {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return false, false
	}
	switch raw {
	case "1", "true", "yes", "on", "y", "t":
		return true, true
	case "0", "false", "no", "off", "n", "f":
		return false, true
	default:
		slog.Warn("Ignoring unsupported env var value", "var", key, "value", raw)
		return false, false
	}
}

// readIntEnv reads an env var as int, returning (value, found).
func readIntEnv(key string) (int, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("Ignoring unsupported env var value", "var", key, "value", raw)
		return 0, false
	}
	return v, true
}

// IsConfigured reports whether URL + key are present.
func (c *Client) IsConfigured() bool {
	return c.serverURL() != "" && c.key() != ""
}

// IsEnabled reports whether Apprise is enabled and configured.
func (c *Client) IsEnabled() bool {
	return c.settings.Enabled && c.IsConfigured()
}

// IsEventEnabled checks whether the event is enabled in config.
func (c *Client) IsEventEnabled(event string) bool {
	switch event {
	case EventPrintStarted:
		return c.settings.Events.PrintStarted
	case EventPrintFinished:
		return c.settings.Events.PrintFinished
	case EventPrintFailed:
		return c.settings.Events.PrintFailed
	case EventPrintPaused:
		return c.settings.Events.PrintPaused
	case EventPrintResumed:
		return c.settings.Events.PrintResumed
	case EventGCodeUploaded:
		return c.settings.Events.GcodeUploaded
	case EventPrintProgress:
		return c.settings.Events.PrintProgress
	default:
		return false
	}
}

// SendEvent renders template/title/type and posts a notification.
func (c *Client) SendEvent(ctx context.Context, event string, payload map[string]any, attachments []string) (bool, string) {
	if !c.IsEnabled() {
		return false, "Apprise is disabled or missing required settings"
	}
	if !c.IsEventEnabled(event) {
		return false, fmt.Sprintf("Event disabled: %s", event)
	}

	tmpl := c.templateForEvent(event)
	body := RenderTemplate(tmpl, payload)
	return c.Post(ctx, EventTitle(event), body, EventType(event), attachments)
}

// Post sends a raw notification payload.
// Attachments that are local file paths are uploaded as multipart/form-data.
// Other attachments (data URIs, URLs) are sent inline as JSON.
func (c *Client) Post(ctx context.Context, title, body, typ string, attachments []string) (bool, string) {
	if !c.IsConfigured() {
		return false, "Apprise server URL or key missing"
	}
	url := c.notifyURL()
	if url == "" {
		return false, "Apprise server URL or key missing"
	}

	// Separate local file paths from inline attachments (data URIs, URLs).
	var localFiles, inlineAttach []string
	for _, a := range attachments {
		if a == "" {
			continue
		}
		if strings.HasPrefix(a, "data:") || strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://") {
			inlineAttach = append(inlineAttach, a)
		} else if _, err := os.Stat(a); err == nil {
			localFiles = append(localFiles, a)
		} else {
			inlineAttach = append(inlineAttach, a)
		}
	}

	// If we have local files, try multipart upload first (Python _post_with_attachments).
	if len(localFiles) > 0 {
		ok, msg, tried := c.postMultipart(ctx, url, title, body, typ, localFiles)
		if tried {
			return ok, msg
		}
		// Fall through to JSON if multipart failed to build.
	}

	// JSON payload path.
	payload := map[string]any{
		"title": title,
		"body":  body,
		"type":  typ,
	}
	if tag := strings.TrimSpace(c.settings.Tag); tag != "" {
		payload["tag"] = tag
	}
	if len(inlineAttach) > 0 {
		payload["attach"] = inlineAttach
	}

	bodyJSON, err := json.Marshal(payload)
	if err != nil {
		return false, fmt.Sprintf("marshal apprise payload: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyJSON))
	if err != nil {
		return false, fmt.Sprintf("build apprise request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return false, ctx.Err().Error()
		}
		return false, err.Error()
	}
	defer resp.Body.Close()

	return parseResponse(resp)
}

// postMultipart uploads local files as multipart/form-data.
// Returns (ok, msg, tried) where tried=false means the caller should fall through.
func (c *Client) postMultipart(ctx context.Context, url, title, body, typ string, files []string) (bool, string, bool) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	_ = writer.WriteField("title", title)
	_ = writer.WriteField("body", body)
	_ = writer.WriteField("type", typ)
	if tag := strings.TrimSpace(c.settings.Tag); tag != "" {
		_ = writer.WriteField("tag", tag)
	}

	attachCount := 0
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			slog.Warn("Apprise attachment not found", "path", path, "error", err)
			continue
		}
		attachCount++
		fieldName := fmt.Sprintf("attach%d", attachCount)
		part, err := writer.CreateFormFile(fieldName, filepath.Base(path))
		if err != nil {
			f.Close()
			continue
		}
		_, _ = io.Copy(part, f)
		f.Close()
	}

	if attachCount == 0 {
		return false, "", false // No files added, fall through to JSON.
	}

	_ = writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return false, fmt.Sprintf("build multipart request: %v", err), true
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return false, ctx.Err().Error(), true
		}
		return false, err.Error(), true
	}
	defer resp.Body.Close()

	ok, msg := parseResponse(resp)
	return ok, msg, true
}

func parseResponse(resp *http.Response) (bool, string) {
	var data map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&data)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if msg := mapMessage(data); msg != "" {
			return false, msg
		}
		return false, fmt.Sprintf("%d %s", resp.StatusCode, resp.Status)
	}

	if success, ok := data["success"].(bool); ok && !success {
		if msg := mapMessage(data); msg != "" {
			return false, msg
		}
		return false, "Apprise error"
	}

	if msg, ok := data["message"].(string); ok && msg != "" {
		return true, msg
	}
	return true, "Notification sent"
}

func mapMessage(data map[string]any) string {
	if data == nil {
		return ""
	}
	if msg, ok := data["error"].(string); ok && msg != "" {
		return msg
	}
	if msg, ok := data["message"].(string); ok && msg != "" {
		return msg
	}
	return ""
}

func (c *Client) templateForEvent(event string) string {
	templates := c.settings.Templates
	switch event {
	case EventPrintStarted:
		if templates.PrintStarted != "" {
			return templates.PrintStarted
		}
	case EventPrintFinished:
		if templates.PrintFinished != "" {
			return templates.PrintFinished
		}
	case EventPrintFailed:
		if templates.PrintFailed != "" {
			return templates.PrintFailed
		}
	case EventPrintPaused:
		if templates.PrintPaused != "" {
			return templates.PrintPaused
		}
	case EventPrintResumed:
		if templates.PrintResumed != "" {
			return templates.PrintResumed
		}
	case EventGCodeUploaded:
		if templates.GcodeUploaded != "" {
			return templates.GcodeUploaded
		}
	case EventPrintProgress:
		if templates.PrintProgress != "" {
			return templates.PrintProgress
		}
	}
	return DefaultTemplateForEvent(event)
}

func (c *Client) serverURL() string {
	return strings.TrimRight(strings.TrimSpace(c.settings.ServerURL), "/")
}

func (c *Client) key() string {
	return strings.Trim(strings.TrimSpace(c.settings.Key), "/")
}

func (c *Client) notifyURL() string {
	serverURL := c.serverURL()
	key := c.key()
	if serverURL == "" || key == "" {
		return ""
	}
	base := serverURL
	if !strings.HasSuffix(base, "/notify") {
		base += "/notify"
	}
	full := base + "/" + key
	u, err := url.Parse(full)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		slog.Warn("Apprise server URL has unsupported scheme, ignoring", "url", full)
		return ""
	}
	return full
}
