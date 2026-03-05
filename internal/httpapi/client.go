package httpapi

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// hostsByRegion maps region codes to Anker cloud API hostnames.
var hostsByRegion = map[string]string{
	"eu": "make-app-eu.ankermake.com",
	"us": "make-app.ankermake.com",
}

const defaultTimeout = 15 * time.Second

// APIError represents an error returned by the Anker cloud API.
type APIError struct {
	Message    string
	StatusCode int
	JSON       map[string]any
}

func (e *APIError) Error() string {
	if e.JSON != nil {
		return fmt.Sprintf("api error: %s (code %d, json: %v)", e.Message, e.StatusCode, e.JSON)
	}
	return fmt.Sprintf("api error: %s (code %d)", e.Message, e.StatusCode)
}

// ClientConfig holds options for creating an API client.
type ClientConfig struct {
	AuthToken string
	UserID    string
	Region    string
	BaseURL   string
	Verify    bool // TLS verification (default: true)
}

// Client is the base Anker cloud HTTP API client.
type Client struct {
	authToken string
	userID    string
	baseURL   string
	scope     string
	http      *http.Client
	log       *slog.Logger
}

// NewClient creates a new base API client.
// If BaseURL is empty, it is derived from the Region.
func NewClient(cfg ClientConfig, scope string) (*Client, error) {
	base := cfg.BaseURL
	if base == "" {
		host, ok := hostsByRegion[cfg.Region]
		if !ok {
			return nil, &APIError{Message: "must specify either base_url or region {'eu', 'us'}"}
		}
		base = "https://" + host
	}
	base = strings.TrimRight(base, "/")

	return &Client{
		authToken: cfg.AuthToken,
		userID:    cfg.UserID,
		baseURL:   base,
		scope:     scope,
		http: &http.Client{
			Timeout: defaultTimeout,
		},
		log: slog.With("component", "httpapi"),
	}, nil
}

// SetTransport replaces the http.RoundTripper (useful for testing).
func (c *Client) SetTransport(rt http.RoundTripper) {
	c.http.Transport = rt
}

// gtoken returns the Gtoken header value: MD5 hex of user_id.
func (c *Client) gtoken() string {
	if c.userID == "" {
		return ""
	}
	h := md5.Sum([]byte(c.userID))
	return fmt.Sprintf("%x", h)
}

// requireAuth returns an error if no auth token is set.
func (c *Client) requireAuth() error {
	if c.authToken == "" {
		return &APIError{Message: "Missing auth token"}
	}
	return nil
}

// Get performs an authenticated GET request and unwraps the API response.
func (c *Client) Get(ctx context.Context, path string, extraHeaders map[string]string) (any, error) {
	url := c.baseURL + c.scope + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("httpapi: build request: %w", err)
	}
	c.setHeaders(req, extraHeaders)
	return c.doAndUnwrap(req)
}

// Post performs an authenticated POST request and unwraps the API response.
func (c *Client) Post(ctx context.Context, path string, extraHeaders map[string]string, body any) (any, error) {
	url := c.baseURL + c.scope + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("httpapi: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("httpapi: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.setHeaders(req, extraHeaders)
	return c.doAndUnwrap(req)
}

func (c *Client) setHeaders(req *http.Request, extra map[string]string) {
	if gt := c.gtoken(); gt != "" {
		req.Header.Set("Gtoken", gt)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
}

func (c *Client) doAndUnwrap(req *http.Request) (any, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("httpapi: request failed: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, &APIError{
				Message:    fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
				StatusCode: resp.StatusCode,
			}
		}
		return nil, fmt.Errorf("httpapi: decode response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{
			Message:    fmt.Sprintf("API request failed: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
			StatusCode: resp.StatusCode,
			JSON:       result,
		}
	}

	code, _ := result["code"].(float64)
	if int(code) != 0 {
		return nil, &APIError{
			Message:    "API error",
			StatusCode: resp.StatusCode,
			JSON:       result,
		}
	}

	c.log.Debug("API response", "url", req.URL.String(), "code", int(code))
	return result["data"], nil
}

// AuthHeaders returns a map with the X-Auth-Token header.
func (c *Client) AuthHeaders() map[string]string {
	return map[string]string{"X-Auth-Token": c.authToken}
}
