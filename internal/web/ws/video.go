package ws

import (
	"net/http"
	"time"

	"github.com/django1982/ankerctl/internal/service"
	"github.com/gorilla/websocket"
)

// Video streams videoqueue frame events as binary websocket messages.
func (h *Handler) Video(w http.ResponseWriter, r *http.Request) {
	if h.state == nil || !h.state.IsLoggedIn() || h.state.IsUnsupportedDevice() {
		h.rejectForbidden(w, "printer not configured")
		return
	}
	if h.vstate != nil && !h.vstate.VideoSupported() {
		h.rejectForbidden(w, "video not supported")
		return
	}
	if h.services == nil {
		h.rejectUnavailable(w)
		return
	}

	svcRaw, ok := h.services.Get("videoqueue")
	if !ok {
		h.rejectUnavailable(w)
		return
	}

	// Enable video on connect (upstream PR #19): the old code gated on
	// VideoEnabled() already being true, which left the browser stuck on
	// "Loading, Please Wait". Now we enable it here and disable it when
	// the last client disconnects — matching Python's behaviour.
	type videoEnabler interface{ SetVideoEnabled(bool) }
	if ve, ok := svcRaw.(videoEnabler); ok {
		ve.SetVideoEnabled(true)
	}

	svc, err := h.services.Borrow("videoqueue")
	if err != nil {
		h.rejectUnavailable(w)
		return
	}
	defer func() {
		// Disable before Return when we are the last /ws/video client.
		// keepVideoQueueRunning in ServiceManager checks VideoEnabled() to
		// decide whether to keep the service alive when refs reach zero.
		// Calling SetVideoEnabled(false) here lets the service stop cleanly
		// once the last browser tab disconnects.
		if h.services.Refs("videoqueue") == 1 {
			if ve, ok := svcRaw.(videoEnabler); ok {
				ve.SetVideoEnabled(false)
			}
		}
		h.services.Return("videoqueue")
	}()

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	out := make(chan []byte, eventBufferSize)
	unsub := svc.Tap(func(v any) {
		switch msg := v.(type) {
		case service.VideoFrameEvent:
			frame := append([]byte(nil), msg.Frame...)
			select {
			case out <- frame:
			default:
				// Drop when websocket writer can't keep up.
			}
		case []byte:
			frame := append([]byte(nil), msg...)
			select {
			case out <- frame:
			default:
				// Drop when websocket writer can't keep up.
			}
		}
	})
	defer unsub()

	readDone := make(chan struct{})
	go h.readPump(conn, readDone)

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(writeWait))
			return
		case <-readDone:
			return
		case frame := <-out:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				return
			}
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(writeWait)); err != nil {
				return
			}
		}
	}
}
