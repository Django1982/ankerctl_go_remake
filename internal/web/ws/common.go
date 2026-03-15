package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/django1982/ankerctl/internal/service"
	"github.com/gorilla/websocket"
)

const (
	eventBufferSize = 32
	pingInterval    = 30 * time.Second
	pongWait        = 60 * time.Second
	writeWait       = 10 * time.Second
)

type state interface {
	APIKey() string
	IsLoggedIn() bool
	IsUnsupportedDevice() bool
}

type videoState interface {
	VideoSupported() bool
}

// ppppSharedProbe holds PPPP probe state shared across all /ws/pppp-state
// clients. This ensures at most one probe goroutine runs at a time regardless
// of how many browser tabs are open simultaneously (upstream PR #17).
type ppppSharedProbe struct {
	mu          sync.Mutex
	result      *bool     // nil = never probed or reset after connect
	lastTime    time.Time // when the last probe completed
	failCount   int       // consecutive failures since last success
	running     bool      // probe goroutine is active
	clientCount int       // active /ws/pppp-state connections
}

// Handler serves websocket endpoints backed by services.
type Handler struct {
	services  *service.ServiceManager
	state     state
	vstate    videoState
	log       *slog.Logger
	upgrader  websocket.Upgrader
	ppppProbe *ppppSharedProbe
}

// New builds websocket handlers for route registration.
func New(services *service.ServiceManager, st state, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{
		services:  services,
		state:     st,
		log:       logger.With("component", "ws"),
		upgrader:  websocket.Upgrader{CheckOrigin: func(_ *http.Request) bool { return true }},
		ppppProbe: &ppppSharedProbe{},
	}
	if vs, ok := st.(videoState); ok {
		h.vstate = vs
	}
	return h
}

func (h *Handler) rejectUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "service unavailable"})
}

func (h *Handler) rejectForbidden(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (h *Handler) streamJSON(r *http.Request, w http.ResponseWriter, svc service.Service) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	out := make(chan any, eventBufferSize)
	unsub := svc.Tap(func(v any) {
		select {
		case out <- v:
		default:
		}
	})
	defer unsub()

	h.writePump(r.Context(), conn, out, func(c *websocket.Conn, msg any) error {
		c.SetWriteDeadline(time.Now().Add(writeWait))
		return c.WriteJSON(msg)
	})
}

func (h *Handler) writePump(ctx context.Context, conn *websocket.Conn, out <-chan any, writeFn func(*websocket.Conn, any) error) {
	readDone := make(chan struct{})
	go h.readPump(conn, readDone)

	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(writeWait))
			return
		case <-readDone:
			return
		case msg := <-out:
			if err := writeFn(conn, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(writeWait)); err != nil {
				return
			}
		}
	}
}

func (h *Handler) readPump(conn *websocket.Conn, done chan<- struct{}) {
	defer close(done)

	conn.SetReadLimit(1 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

func boolToStatus(v bool) string {
	if v {
		return "connected"
	}
	return "disconnected"
}
