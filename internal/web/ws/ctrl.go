package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ctrlCloseFrame is a sentinel value sent through the out channel to request
// that ctrlWritePump emit a WebSocket close frame with a specific code and
// reason. This keeps all conn.Write* calls on a single goroutine (the write
// pump) so there are never two concurrent writers on the connection.
type ctrlCloseFrame struct {
	code   int
	reason string
}

// Ctrl handles bidirectional websocket controls with inline API key auth.
//
// Goroutine topology (all conn reads on goroutine A, all conn writes on goroutine B):
//
//	Goroutine A (auth + read loop): authenticateCtrl → ctrlReadLoop
//	Goroutine B (write pump):       ctrlWritePump — sole writer of conn
//
// Communication from A to B is via the out channel. Disconnection is signalled
// via readDone.
func (h *Handler) Ctrl(w http.ResponseWriter, r *http.Request) {
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
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	readDone := make(chan struct{})

	// Goroutine A: sole reader of conn.
	go func() {
		defer close(readDone)
		// Send initial state immediately on connect (mirrors Python behaviour).
		// HTTP-level auth is already enforced by middleware.
		h.sendCtrlInitialState(out)
		h.ctrlReadLoop(ctx, conn, out)
		cancel()
	}()

	// Goroutine B: sole writer of conn.
	h.ctrlWritePump(ctx, conn, out, readDone)
	<-readDone
}

// ctrlWritePump is the sole writer for the Ctrl websocket connection.
// It handles ping keepalives, JSON message serialisation, and close frames.
// It returns when ctx is cancelled, readDone is closed, or a write error occurs.
//
// Priority rule: pending messages in out are always drained before acting on
// ctx.Done() or readDone. This guarantees that a ctrlCloseFrame enqueued by
// the auth goroutine is delivered even if ctx is cancelled immediately after.
func (h *Handler) ctrlWritePump(ctx context.Context, conn *websocket.Conn, out <-chan any, readDone <-chan struct{}) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		// High-priority: drain any pending message first before checking stop signals.
		select {
		case msg := <-out:
			if stop := h.ctrlHandleMsg(conn, msg); stop {
				return
			}
			continue
		default:
		}

		select {
		case <-ctx.Done():
			// Drain one final message in case a close frame arrived concurrently.
			select {
			case msg := <-out:
				_ = h.ctrlHandleMsg(conn, msg)
			default:
				_ = conn.WriteControl(
					websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
					time.Now().Add(writeWait),
				)
			}
			return
		case <-readDone:
			// Drain any pending close frame before exiting.
			select {
			case msg := <-out:
				_ = h.ctrlHandleMsg(conn, msg)
			default:
			}
			return
		case msg := <-out:
			if stop := h.ctrlHandleMsg(conn, msg); stop {
				return
			}
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(writeWait)); err != nil {
				return
			}
		}
	}
}

// ctrlHandleMsg processes one outbound message. Returns true if the pump should stop.
func (h *Handler) ctrlHandleMsg(conn *websocket.Conn, msg any) (stop bool) {
	if cf, ok := msg.(ctrlCloseFrame); ok {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(cf.code, cf.reason),
			time.Now().Add(writeWait),
		)
		return true
	}
	conn.SetWriteDeadline(time.Now().Add(writeWait))
	if err := conn.WriteJSON(msg); err != nil {
		return true
	}
	return false
}

// sendCtrlInitialState sends the ankerctl handshake and current video profile
// via the out channel (never directly to conn).
//
// Python behaviour: after auth the server immediately sends {"ankerctl":1}
// followed by {"video_profile": <current_profile_id>}.
func (h *Handler) sendCtrlInitialState(out chan<- any) {
	select {
	case out <- map[string]any{"ankerctl": 1}:
	default:
	}

	if h.services == nil {
		return
	}
	svc, ok := h.services.Get("videoqueue")
	if !ok {
		// No video service — send default so the client always has state.
		select {
		case out <- map[string]any{"video_profile": "sd"}:
		default:
		}
		return
	}
	type profileGetter interface {
		CurrentProfile() string
	}
	profile := "sd"
	if pg, ok := svc.(profileGetter); ok {
		if p := pg.CurrentProfile(); p != "" {
			profile = p
		}
	}
	select {
	case out <- map[string]any{"video_profile": profile}:
	default:
	}
}

func (h *Handler) ctrlReadLoop(ctx context.Context, conn *websocket.Conn, out chan<- any) {
	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}

		resp := h.handleCtrlCommand(payload)
		if resp == nil {
			continue
		}
		select {
		case out <- resp:
		default:
		}
	}
}

func (h *Handler) handleCtrlCommand(payload []byte) map[string]any {
	if h.services == nil {
		return map[string]any{"error": "service manager unavailable"}
	}

	var msg map[string]any
	if err := json.Unmarshal(payload, &msg); err != nil {
		return map[string]any{"error": "malformed json"}
	}
	if len(msg) == 0 {
		return nil
	}

	if raw, ok := msg["light"]; ok {
		on, ok := raw.(bool)
		if !ok {
			return map[string]any{"error": "light value must be boolean"}
		}
		svc, err := h.services.Borrow("videoqueue")
		if err != nil {
			return map[string]any{"error": "videoqueue unavailable"}
		}
		defer h.services.Return("videoqueue")
		ls, ok := svc.(interface {
			SetLight(context.Context, bool) error
		})
		if !ok {
			return map[string]any{"error": "videoqueue does not support light control"}
		}
		if err := ls.SetLight(context.Background(), on); err != nil {
			return map[string]any{"error": err.Error()}
		}
	}

	if raw, ok := msg["video_profile"]; ok {
		profile, ok := raw.(string)
		if !ok {
			return map[string]any{"error": "video_profile value must be sd|hd|fhd"}
		}
		profile = strings.ToLower(strings.TrimSpace(profile))
		if profile != "sd" && profile != "hd" && profile != "fhd" {
			return map[string]any{"error": "video_profile value must be sd|hd|fhd"}
		}
		svc, err := h.services.Borrow("videoqueue")
		if err != nil {
			return map[string]any{"error": "videoqueue unavailable"}
		}
		defer h.services.Return("videoqueue")
		ps, ok := svc.(interface{ SetProfile(string) error })
		if !ok {
			return map[string]any{"error": "videoqueue does not support profile control"}
		}
		if err := ps.SetProfile(profile); err != nil {
			return map[string]any{"error": err.Error()}
		}
	}

	if raw, ok := msg["quality"]; ok {
		mode, err := parseIntValue(raw)
		if err != nil {
			return map[string]any{"error": "quality value must be int"}
		}
		svc, err := h.services.Borrow("videoqueue")
		if err != nil {
			return map[string]any{"error": "videoqueue unavailable"}
		}
		defer h.services.Return("videoqueue")
		vm, ok := svc.(interface{ SetVideoMode(int) error })
		if !ok {
			return map[string]any{"error": "videoqueue does not support quality control"}
		}
		if err := vm.SetVideoMode(mode); err != nil {
			return map[string]any{"error": err.Error()}
		}
	}

	if raw, ok := msg["video_enabled"]; ok {
		enabled, ok := raw.(bool)
		if !ok {
			return map[string]any{"error": "video_enabled value must be boolean"}
		}
		svc, err := h.services.Borrow("videoqueue")
		if err != nil {
			return map[string]any{"error": "videoqueue unavailable"}
		}
		defer h.services.Return("videoqueue")
		ve, ok := svc.(interface{ SetVideoEnabled(bool) })
		if !ok {
			return map[string]any{"error": "videoqueue does not support enable control"}
		}
		ve.SetVideoEnabled(enabled)
	}

	return nil
}

func parseIntValue(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case float32:
		return int(n), nil
	case int:
		return n, nil
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	case string:
		return strconv.Atoi(strings.TrimSpace(n))
	default:
		return 0, strconv.ErrSyntax
	}
}
