package ws

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/django1982/ankerctl/internal/service"
	"github.com/gorilla/websocket"
)

type mqttMessageTimeProvider interface {
	LastMessageTime() time.Time
}

type ppppProbeState interface {
	ProbePPPP(context.Context) bool
}

const (
	ppppProbeInterval  = 60 * time.Second
	ppppRetryInterval  = 15 * time.Second
	ppppMQTTStaleAfter = 30 * time.Second
	ppppKeepaliveEvery = 10 * time.Second
	ppppStateTick      = time.Second
	ppppMaxRetries     = 2
)

// PPPPState sends PPPP connection status using passive service reads plus a
// background LAN probe, matching the Python web UI semantics.
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

	type ppppStatusSnapshot struct {
		mu             sync.Mutex
		lastStatus     string
		lastKeepalive  time.Time
		wasConnected   bool
		lastProbeTime  time.Time
		probeResult    *bool
		probeRunning   bool
		probeFailCount int
		mqttWasStale   bool
	}

	snapshot := &ppppStatusSnapshot{}

	startProbe := func() {
		prober, ok := h.state.(ppppProbeState)
		if !ok {
			return
		}
		snapshot.mu.Lock()
		if snapshot.probeRunning {
			snapshot.mu.Unlock()
			return
		}
		snapshot.probeRunning = true
		snapshot.mu.Unlock()

		go func() {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()

			ok := prober.ProbePPPP(ctx)

			snapshot.mu.Lock()
			snapshot.probeRunning = false
			snapshot.lastProbeTime = time.Now()
			snapshot.probeResult = new(bool)
			*snapshot.probeResult = ok
			if ok {
				snapshot.probeFailCount = 0
			} else {
				snapshot.probeFailCount++
			}
			snapshot.mu.Unlock()
		}()
	}

	emitStatus := func(status string, serviceState service.RunState) {
		msg := map[string]any{
			"status":        status,
			"service_state": int(serviceState),
		}
		select {
		case out <- msg:
		default:
		}
	}

	startProbe()

	pollStatus := func() {
		now := time.Now()
		currentStatus := "dormant"
		currentServiceState := service.StateStopped

		var (
			ppppConnected    bool
			ppppSvcAvailable bool
			mqttLastMessage  time.Time
		)
		if h.services != nil {
			if svc, ok := h.services.Get("ppppservice"); ok {
				currentServiceState = svc.State()
				ppppSvcAvailable = true
				if p, ok := svc.(interface{ IsConnected() bool }); ok {
					ppppConnected = p.IsConnected()
				}
			}
			if svc, ok := h.services.Get("mqttqueue"); ok {
				if mt, ok := svc.(mqttMessageTimeProvider); ok {
					mqttLastMessage = mt.LastMessageTime()
				}
			}
		}

		var shouldProbe bool

		snapshot.mu.Lock()

		if ppppSvcAvailable && ppppConnected {
			currentStatus = "connected"
			snapshot.wasConnected = true
			snapshot.probeResult = nil
			snapshot.probeFailCount = 0
		} else {
			mqttStale := !mqttLastMessage.IsZero() && now.Sub(mqttLastMessage) > ppppMQTTStaleAfter
			mqttRecovered := snapshot.mqttWasStale && !mqttStale
			if mqttRecovered {
				snapshot.probeResult = nil
				snapshot.probeFailCount = 0
			}
			snapshot.mqttWasStale = mqttStale

			nextInterval := ppppRetryInterval
			if snapshot.probeFailCount > ppppMaxRetries {
				nextInterval = ppppProbeInterval
			}
			probeSucceeded := snapshot.probeResult != nil && *snapshot.probeResult
			probeFailed := snapshot.probeResult != nil && !*snapshot.probeResult
			shouldProbe = !snapshot.probeRunning &&
				(snapshot.lastProbeTime.IsZero() ||
					((mqttStale || mqttRecovered || probeFailed) &&
						now.Sub(snapshot.lastProbeTime) > nextInterval))

			switch {
			case probeSucceeded:
				currentStatus = "connected"
			case probeFailed:
				currentStatus = "disconnected"
			case ppppSvcAvailable && currentServiceState != service.StateStopped && snapshot.wasConnected:
				currentStatus = "disconnected"
			default:
				currentStatus = "dormant"
				if !ppppSvcAvailable || currentServiceState == service.StateStopped {
					snapshot.wasConnected = false
				}
			}
		}

		if currentStatus != snapshot.lastStatus || (currentStatus == "connected" && now.Sub(snapshot.lastKeepalive) >= ppppKeepaliveEvery) {
			if snapshot.lastStatus == "" &&
				currentStatus == "dormant" &&
				snapshot.probeRunning &&
				snapshot.probeResult == nil &&
				!ppppSvcAvailable {
				snapshot.mu.Unlock()
				return
			}
			snapshot.lastStatus = currentStatus
			if currentStatus == "connected" {
				snapshot.lastKeepalive = now
			}
			emitStatus(currentStatus, currentServiceState)
		}
		snapshot.mu.Unlock()
		if shouldProbe {
			startProbe()
		}
	}
	pollStatus()

	go func() {
		ticker := time.NewTicker(ppppStateTick)
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
