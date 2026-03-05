package handler

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

// TimelapseList returns available timelapse files.
func (h *Handler) TimelapseList(w http.ResponseWriter, _ *http.Request) {
	tl, ok := h.timelapse()
	if !ok {
		h.writeJSON(w, http.StatusOK, map[string]any{"videos": []string{}, "enabled": false})
		return
	}
	videos, err := tl.ListVideos()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to list timelapses")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"videos": videos, "enabled": true})
}

// TimelapseDownload returns a timelapse mp4 as attachment.
func (h *Handler) TimelapseDownload(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if filename == "" || strings.Contains(filename, "..") || strings.ContainsAny(filename, `/\\`) || filepath.Base(filename) != filename {
		h.writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	tl, ok := h.timelapse()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "timelapse service unavailable")
		return
	}
	path, ok := tl.GetVideoPath(filename)
	if !ok {
		h.writeError(w, http.StatusNotFound, "Video not found")
		return
	}
	// Python: send_file(..., as_attachment=False) → Content-Disposition: inline
	w.Header().Set("Content-Disposition", "inline; filename="+filename)
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeFile(w, r, path)
}

// TimelapseDelete deletes a timelapse video.
func (h *Handler) TimelapseDelete(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if filename == "" || strings.Contains(filename, "..") || strings.ContainsAny(filename, `/\\`) || filepath.Base(filename) != filename {
		h.writeError(w, http.StatusBadRequest, "invalid filename")
		return
	}
	tl, ok := h.timelapse()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "timelapse service unavailable")
		return
	}
	if !tl.DeleteVideo(filename) {
		h.writeError(w, http.StatusNotFound, "Video not found")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
