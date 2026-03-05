package handler

import "net/http"

// BedLevelingLive queries bed-leveling data from printer.
func (h *Handler) BedLevelingLive(w http.ResponseWriter, _ *http.Request) {
	// TODO(phase-13): implement short-lived MQTT query and BL-Grid parsing.
	h.writeError(w, http.StatusNotImplemented, "bed leveling read not implemented")
}

// BedLevelingLast returns most recent persisted bed-leveling grid.
func (h *Handler) BedLevelingLast(w http.ResponseWriter, _ *http.Request) {
	// TODO(phase-13): implement persisted .bed lookup.
	h.writeError(w, http.StatusNotImplemented, "bed leveling history not implemented")
}
