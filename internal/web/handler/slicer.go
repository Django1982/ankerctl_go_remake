package handler

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/django1982/ankerctl/internal/model"
)

func parseBoolHTTP(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// SlicerUpload handles OctoPrint-compatible multipart file uploads.
func (h *Handler) SlicerUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	fd, hdr, err := r.FormFile("file")
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "missing file field")
		return
	}
	defer fd.Close()

	data, err := io.ReadAll(fd)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "failed to read uploaded file")
		return
	}

	startPrint := parseBoolHTTP(r.FormValue("print"))
	cfg, _ := h.loadConfig()
	userID := ""
	rateLimit := 10
	if cfg != nil {
		if cfg.Account != nil {
			userID = cfg.Account.UserID
		}
		if cfg.UploadRateMbps > 0 {
			rateLimit = cfg.UploadRateMbps
		}
	}
	if v := strings.TrimSpace(r.FormValue("upload_rate_mbps")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rateLimit = n
		}
	}

	// Borrow ppppservice so it starts and connects before the upload begins.
	// Python parity: filetransfer.py calls pppp_open() which waits for
	// StateConnected before sending any data.
	if _, err := h.svc.Borrow("ppppservice"); err != nil {
		h.writeError(w, http.StatusServiceUnavailable, "pppp service unavailable")
		return
	}
	defer h.svc.Return("ppppservice")

	// Borrow filetransfer so its WorkerRun loop is active to process the request.
	if _, err := h.svc.Borrow("filetransfer"); err != nil {
		h.writeError(w, http.StatusServiceUnavailable, "file transfer service unavailable")
		return
	}
	defer h.svc.Return("filetransfer")

	ft, ok := h.fileTransfer()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "file transfer service unavailable")
		return
	}
	userName := strings.TrimSpace(r.UserAgent())
	if userName == "" {
		userName = "ankerctl"
	}
	if err := ft.SendFile(r.Context(), hdr.Filename, userName, userID, data, rateLimit, startPrint); err != nil {
		h.writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	// Python parity: return effective rate and source after successful upload.
	cfgRate := 0
	if cfg != nil {
		cfgRate = cfg.UploadRateMbps
	}
	effectiveRate, rateSource := model.ResolveUploadRateMbpsWithSource(cfgRate, 0)
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status":             "ok",
		"upload_rate_mbps":   effectiveRate,
		"upload_rate_source": rateSource,
	})
}
