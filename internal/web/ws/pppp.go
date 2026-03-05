package ws

import (
	"net/http"
	"time"

	"github.com/django1982/ankerctl/internal/service"
	"github.com/gorilla/websocket"
)

// PPPPState sends PPPP connection status JSON every 2 seconds.
func (h *Handler) PPPPState(w http.ResponseWriter, r *http.Request) {
	if h.state == nil || !h.state.IsLoggedIn() || h.state.IsUnsupportedDevice() {
		h.rejectForbidden(w, "printer not configured")
		return
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	out := make(chan any, eventBufferSize)
	pollStatus := func() {
		status := map[string]any{"status": "dormant"}
		if h.services != nil {
			if svc, ok := h.services.Get("ppppservice"); ok {
				status["service_state"] = int(svc.State())
				if p, ok := svc.(interface{ IsConnected() bool }); ok {
					if svc.State() == service.StateRunning {
						status["status"] = boolToStatus(p.IsConnected())
					} else {
						status["status"] = "dormant"
					}
				}
			}
		}
		select {
		case out <- status:
		default:
		}
	}
	pollStatus()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-ticker.C:
				pollStatus()
			}
		}
	}()

	h.writePump(r.Context(), conn, out, func(c *websocket.Conn, msg any) error {
		c.SetWriteDeadline(time.Now().Add(writeWait))
		return c.WriteJSON(msg)
	})
}
