package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/django1982/ankerctl/internal/model"
)

const defaultTimeout = 10 * time.Second

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
func (c *Client) Post(ctx context.Context, title, body, typ string, attachments []string) (bool, string) {
	if !c.IsConfigured() {
		return false, "Apprise server URL or key missing"
	}
	url := c.notifyURL()
	if url == "" {
		return false, "Apprise server URL or key missing"
	}

	payload := map[string]any{
		"title": title,
		"body":  body,
		"type":  typ,
	}
	if tag := strings.TrimSpace(c.settings.Tag); tag != "" {
		payload["tag"] = tag
	}
	if len(attachments) > 0 {
		payload["attach"] = attachments
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
	return base + "/" + key
}
