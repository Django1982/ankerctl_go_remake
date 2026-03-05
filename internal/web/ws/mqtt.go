package ws

import (
	"net/http"
)

// MQTT streams mqttqueue tap events as JSON websocket messages.
func (h *Handler) MQTT(w http.ResponseWriter, r *http.Request) {
	if h.state == nil || !h.state.IsLoggedIn() || h.state.IsUnsupportedDevice() {
		h.rejectForbidden(w, "printer not configured")
		return
	}
	if h.services == nil {
		h.rejectUnavailable(w)
		return
	}

	svc, err := h.services.Borrow("mqttqueue")
	if err != nil {
		h.rejectUnavailable(w)
		return
	}
	defer h.services.Return("mqttqueue")

	h.streamJSON(r, w, svc)
}
