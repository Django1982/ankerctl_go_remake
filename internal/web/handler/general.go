package handler

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/django1982/ankerctl/internal/model"
	"github.com/django1982/ankerctl/internal/service"
)

// Root serves the web UI placeholder.
func (h *Handler) Root(w http.ResponseWriter, r *http.Request) {
	cfg, _ := h.loadConfig()
	printer, activeIdx, locked := h.activePrinter(cfg)

	host, port := requestHostPort(r)
	data := TemplateData{
		ActivePrinterIndex: activeIdx,
		PrinterIndexLocked: locked,
		Configure:          cfg != nil && cfg.IsConfigured(),
		DebugMode:          h.devMode,
		VideoSupported:     h.videoSupported(),
		UnsupportedDevice:  h.isUnsupportedDevice(),
		CountryCodes:       countryCodes,
		RequestHost:        host,
		RequestPort:        port,
	}

	data.UploadRateChoices = model.UploadRateMbpsChoices
	if cfg != nil {
		data.Printers = cfg.Printers
		data.Printer = printer
		// Build the enriched printer list for the selector dropdown.
		pl := make([]PrinterSummary, 0, len(cfg.Printers))
		for i, p := range cfg.Printers {
			pl = append(pl, PrinterSummary{
				Index:     i,
				Name:      p.Name,
				SN:        p.SN,
				Model:     p.Model,
				IPAddr:    p.IPAddr,
				Supported: model.IsPrinterSupported(p.Model),
			})
		}
		data.PrinterList = pl
		data.UploadRateConfig = cfg.UploadRateMbps
		data.AnkerConfig = configShow(cfg)
		if cfg.Account != nil {
			data.ConfigExistingEmail = cfg.Account.Email
			data.CurrentCountry = cfg.Account.Country
		}
	}

	// Resolve effective upload rate (env may override config)
	effectiveRate, rateSource := model.ResolveUploadRateMbpsWithSource(data.UploadRateConfig, 0)
	data.UploadRateMbps = effectiveRate
	data.UploadRateSource = rateSource
	data.UploadRateEnv = os.Getenv("UPLOAD_RATE_MBPS") != ""

	if h.cfg != nil {
		data.LoginFilePath = h.cfg.ConfigDir()
	}

	if err := h.render(w, "base.html", data); err != nil {
		h.log.Error("render root", "error", err)
		h.writeError(w, http.StatusInternalServerError, "rendering failed")
	}
}

// configShow formats a Config as the human-readable text shown in the
// Setup → Account → "AnkerMake M5 Config" panel. Mirrors web/config.py:config_show.
func configShow(cfg *model.Config) string {
	if cfg == nil {
		return "No printers found, please load your login config..."
	}
	a := cfg.Account
	if a == nil {
		return "No printers found, please load your login config..."
	}

	redact := func(s string) string {
		if len(s) < 10 {
			return "[REDACTED]"
		}
		return s[:10] + "...[REDACTED]"
	}

	uploadRate := "unset"
	if cfg.UploadRateMbps != 0 {
		uploadRate = fmt.Sprintf("%d", cfg.UploadRateMbps)
	}

	country := "[REDACTED]"
	if a.Country == "" {
		country = ""
	}

	out := fmt.Sprintf("Account:\n"+
		"  user_id:    %s\n"+
		"  auth_token: %s\n"+
		"  email:      %s\n"+
		"  region:     %s\n"+
		"  country:    %s\n"+
		"  upload_rate_mbps: %s\n\n",
		redact(a.UserID),
		redact(a.AuthToken),
		redact(a.Email),
		strings.ToUpper(a.Region),
		country,
		uploadRate,
	)

	out += "Printers:\n"
	for i, p := range cfg.Printers {
		out += fmt.Sprintf("  printer:   %d\n", i)
		out += fmt.Sprintf("  id:        %s\n", p.ID)
		out += fmt.Sprintf("  name:      %s\n", p.Name)
		out += fmt.Sprintf("  duid:      %s\n", p.P2PDUID)
		out += fmt.Sprintf("  sn:        %s\n", p.SN)
		out += fmt.Sprintf("  model:     %s\n", p.Model)
		if !p.CreateTime.IsZero() {
			out += fmt.Sprintf("  created:   %s\n", p.CreateTime.Format("2006-01-02 15:04:05"))
		}
		if !p.UpdateTime.IsZero() {
			out += fmt.Sprintf("  updated:   %s\n", p.UpdateTime.Format("2006-01-02 15:04:05"))
		}
		out += fmt.Sprintf("  ip:        %s\n", p.IPAddr)
		out += fmt.Sprintf("  wifi_mac:  %s\n", prettyMAC(p.WifiMAC))
		out += "  api_hosts:\n"
		for _, h := range splitHosts(p.APIHosts) {
			out += fmt.Sprintf("     - %s\n", h)
		}
		out += "  p2p_hosts:\n"
		for _, h := range splitHosts(p.P2PHosts) {
			out += fmt.Sprintf("     - %s\n", h)
		}
	}
	return out
}

func splitHosts(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func prettyMAC(mac string) string {
	// Already formatted or empty — return as-is.
	return mac
}

func requestHostPort(r *http.Request) (host, port string) {
	h, p, err := net.SplitHostPort(r.Host)
	if err != nil {
		return r.Host, ""
	}
	return h, p
}

// Health is a lightweight liveness endpoint.
func (h *Handler) Health(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Version returns the API version payload (OctoPrint-compatible shape).
func (h *Handler) Version(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"api": "0.1", "server": "1.9.0", "text": "OctoPrint 1.9.0"})
}

// Video streams H.264 frames from the VideoQueue as a chunked video/mp4 response.
// Mirrors Python web/__init__.py:video_download().
func (h *Handler) Video(w http.ResponseWriter, r *http.Request) {
	forTimelapse := r.URL.Query().Get("for_timelapse") == "1"

	// Check that a printer is configured.
	cfg, _ := h.loadConfig()
	if cfg == nil || !cfg.IsConfigured() {
		return // empty response, matching Python's generate() that yields nothing
	}

	if h.svc == nil {
		return
	}

	// Look up videoqueue.
	vq, ok := h.videoQueue()
	if !ok {
		return
	}

	// Unless this is a timelapse capture, require video_enabled.
	if !forTimelapse && !vq.VideoEnabled() {
		return
	}

	// If the service is stopped, try to start it via Borrow.
	if vq.State() == service.StateStopped {
		if _, err := h.svc.Borrow("videoqueue"); err != nil {
			return
		}
		defer h.svc.Return("videoqueue")
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)

	frameCh := make(chan []byte, 64)
	unsub := vq.Tap(func(v any) {
		msg, ok := v.(service.VideoFrameEvent)
		if !ok {
			return
		}
		frame := append([]byte(nil), msg.Frame...)
		select {
		case frameCh <- frame:
		default:
			// Drop when HTTP writer can't keep up.
		}
	})
	defer unsub()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case frame := <-frameCh:
			if _, err := w.Write(frame); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		}
	}
}

// Snapshot captures a JPEG from VideoQueue and serves it as attachment.
func (h *Handler) Snapshot(w http.ResponseWriter, r *http.Request) {
	vq, ok := h.videoQueue()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "video service not available")
		return
	}
	tmp, err := os.CreateTemp("", "ankerctl_snapshot_*.jpg")
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to create temp file")
		return
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(path)

	if err := vq.CaptureSnapshot(r.Context(), path); err != nil {
		h.writeError(w, http.StatusInternalServerError, fmt.Sprintf("snapshot failed: %v", err))
		return
	}
	ts := time.Now().Format("20060102_150405")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=ankerctl_snapshot_%s.jpg", ts))
	http.ServeFile(w, r, path)
}
