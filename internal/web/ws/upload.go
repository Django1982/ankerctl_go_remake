package ws

import (
	"net/http"
)

// Upload streams filetransfer tap events as JSON websocket messages.
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	if h.state == nil || !h.state.IsLoggedIn() || h.state.IsUnsupportedDevice() {
		h.rejectForbidden(w, "printer not configured")
		return
	}
	if h.services == nil {
		h.rejectUnavailable(w)
		return
	}

	svc, err := h.services.Borrow("filetransfer")
	if err != nil {
		h.rejectUnavailable(w)
		return
	}
	defer h.services.Return("filetransfer")

	h.streamJSON(r, w, svc)
}
