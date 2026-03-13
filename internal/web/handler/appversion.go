package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const ghReleasesAPI = "https://api.github.com/repos/Django1982/ankerctl_go_remake/releases/latest"

// releaseCache fetches the latest GitHub release tag once in the background.
type releaseCache struct {
	mu      sync.RWMutex
	latest  string
	checked bool
}

// Prefetch kicks off the GitHub check in a background goroutine.
func (rc *releaseCache) Prefetch() {
	go func() {
		latest := rc.fetchFromGitHub()
		rc.mu.Lock()
		rc.latest = latest
		rc.checked = true
		rc.mu.Unlock()
	}()
}

func (rc *releaseCache) get() (latest string, checked bool) {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.latest, rc.checked
}

func (rc *releaseCache) fetchFromGitHub() string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ghReleasesAPI, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ankerctl-version-check")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Debug("version check: GitHub unreachable", "err", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("version check: unexpected status", "status", resp.StatusCode)
		return ""
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.TagName)
}

// WithVersion injects the running version string and starts the background
// GitHub release check.
func (h *Handler) WithVersion(v string) {
	h.version = v
	h.releases = &releaseCache{}
	if v != "" && v != "dev" {
		h.releases.Prefetch()
	}
}

// AppVersion returns the running version, the latest GitHub release (if
// already fetched), and whether an update is available.
// GET /api/ankerctl/version
func (h *Handler) AppVersion(w http.ResponseWriter, _ *http.Request) {
	current := h.version
	if current == "" {
		current = "dev"
	}

	latest := ""
	updateAvailable := false
	if h.releases != nil {
		latest, _ = h.releases.get()
		if latest != "" && latest != current {
			updateAvailable = true
		}
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
		"current":          current,
		"latest":           latest,
		"update_available": updateAvailable,
	})
}
