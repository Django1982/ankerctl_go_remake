package handler

import "net/http"

// BedLevelingLive queries bed-leveling data from printer.
func (h *Handler) BedLevelingLive(w http.ResponseWriter, r *http.Request) {
	q, ok := h.mqttQueue()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "mqtt service not available")
		return
	}
	if err := q.QueryBedLeveling(r.Context()); err != nil {
		h.writeError(w, http.StatusInternalServerError, "failed to query bed leveling: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "query sent"})
}

// BedLevelingLast returns most recent persisted bed-leveling grid.
func (h *Handler) BedLevelingLast(w http.ResponseWriter, _ *http.Request) {
	q, ok := h.mqttQueue()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "mqtt service not available")
		return
	}
	grid := q.LastBedLevelingGrid()
	if len(grid) == 0 {
		h.writeError(w, http.StatusNotFound, "no bed leveling data available")
		return
	}
	h.writeJSON(w, http.StatusOK, grid)
}
