package handler

import "net/http"

// BedLevelingLive reads the bed-leveling grid from the printer by sending
// "M420 V" GCode and collecting the response for ~4 s. Returns the parsed
// grid with min/max statistics. Mirrors Python _read_bed_leveling_grid().
func (h *Handler) BedLevelingLive(w http.ResponseWriter, r *http.Request) {
	q, ok := h.mqttQueue()
	if !ok {
		h.writeError(w, http.StatusServiceUnavailable, "mqtt service not available")
		return
	}
	grid, err := q.QueryBedLeveling(r.Context())
	if err != nil {
		h.writeError(w, http.StatusGatewayTimeout, "failed to read bed leveling grid: "+err.Error())
		return
	}
	h.writeJSON(w, http.StatusOK, grid)
}

// BedLevelingLast returns the most recent persisted bed-leveling grid.
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
