package handler

import (
	"net/http"
	"strconv"
)

// HistoryList returns print history.
// Response shape matches Python: {"entries": [...], "total": N}.
func (h *Handler) HistoryList(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		h.writeJSON(w, http.StatusOK, map[string]any{"entries": []any{}, "total": 0})
		return
	}
	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	records, err := h.db.GetHistory(limit, offset)
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to load history")
		return
	}
	total, err := h.db.HistoryCount()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to count history")
		return
	}

	entries := make([]map[string]any, 0, len(records))
	for _, rec := range records {
		row := map[string]any{
			"id":           rec.ID,
			"filename":     rec.Filename,
			"status":       rec.Status,
			"started_at":   rec.StartedAt,
			"finished_at":  rec.FinishedAt,
			"duration_sec": rec.DurationSec,
			"progress":     rec.Progress,
		}
		entries = append(entries, row)
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"entries": entries, "total": total})
}

// HistoryClear clears print history.
func (h *Handler) HistoryClear(w http.ResponseWriter, _ *http.Request) {
	if h.db == nil {
		h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if err := h.db.ClearHistory(); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to clear history")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
