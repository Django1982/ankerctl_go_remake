package ws

import (
	"context"
	"net/http"
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
// shared background LAN probe. A single probe goroutine runs at a time across
// all connected /ws/pppp-state clients (upstream PR #17).
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

	// Register this client; kick off an immediate probe if we are the first.
	h.ppppProbe.mu.Lock()
	h.ppppProbe.clientCount++
	isFirst := h.ppppProbe.clientCount == 1
	h.ppppProbe.mu.Unlock()
	defer func() {
		h.ppppProbe.mu.Lock()
		h.ppppProbe.clientCount--
		h.ppppProbe.mu.Unlock()
	}()

	// startProbe spawns a probe goroutine if none is running and at least one
	// client is watching. Uses context.Background() so the probe outlives any
	// individual client's request context.
	startProbe := func() {
		prober, ok := h.state.(ppppProbeState)
		if !ok {
			return
		}
		h.ppppProbe.mu.Lock()
		if h.ppppProbe.running || h.ppppProbe.clientCount == 0 {
			h.ppppProbe.mu.Unlock()
			return
		}
		h.ppppProbe.running = true
		h.ppppProbe.mu.Unlock()

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ok := prober.ProbePPPP(ctx)
			h.ppppProbe.mu.Lock()
			h.ppppProbe.running = false
			h.ppppProbe.lastTime = time.Now()
			h.ppppProbe.result = new(bool)
			*h.ppppProbe.result = ok
			if ok {
				h.ppppProbe.failCount = 0
			} else {
				h.ppppProbe.failCount++
			}
			h.ppppProbe.mu.Unlock()
		}()
	}

	if isFirst {
		startProbe()
	}

	// Per-connection state.
	var (
		lastStatus   string
		lastKeepalive time.Time
		wasConnected bool // true once we observe "connected"; resets on dormant
		mqttWasStale bool // tracks previous MQTT stale state to detect recovery
	)

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

		h.ppppProbe.mu.Lock()

		if ppppSvcAvailable && ppppConnected {
			currentStatus = "connected"
			wasConnected = true
			// Reset probe state on actual PPPP connection.
			h.ppppProbe.result = nil
			h.ppppProbe.failCount = 0
		} else {
			mqttStale := !mqttLastMessage.IsZero() && now.Sub(mqttLastMessage) > ppppMQTTStaleAfter
			mqttRecovered := mqttWasStale && !mqttStale
			if mqttRecovered {
				// MQTT just came back — reset probe state so we re-probe immediately.
				h.ppppProbe.result = nil
				h.ppppProbe.failCount = 0
			}
			mqttWasStale = mqttStale

			// Snapshot shared probe values while holding the lock.
			probeResult := h.ppppProbe.result
			lastProbeTime := h.ppppProbe.lastTime
			probeFailCount := h.ppppProbe.failCount

			nextInterval := ppppRetryInterval
			if probeFailCount > ppppMaxRetries {
				nextInterval = ppppProbeInterval
			}

			probeSucceeded := probeResult != nil && *probeResult
			probeFailed := probeResult != nil && !*probeResult

			// Also probe when PPPP was recently connected but the service
			// stopped (e.g. last video client disconnected) so the badge
			// refreshes promptly (upstream PR #19).
			ppppWentDormant := wasConnected && probeResult == nil

			shouldProbe = !h.ppppProbe.running &&
				(lastProbeTime.IsZero() ||
					((mqttStale || mqttRecovered || probeFailed || ppppWentDormant) &&
						now.Sub(lastProbeTime) > nextInterval))

			switch {
			case probeSucceeded:
				currentStatus = "connected"
			case probeFailed:
				currentStatus = "disconnected"
			case ppppSvcAvailable && currentServiceState != service.StateStopped && wasConnected:
				currentStatus = "disconnected"
			default:
				currentStatus = "dormant"
				if !ppppSvcAvailable || currentServiceState == service.StateStopped {
					wasConnected = false
				}
			}
		}

		if currentStatus != lastStatus || (currentStatus == "connected" && now.Sub(lastKeepalive) >= ppppKeepaliveEvery) {
			if lastStatus == "" &&
				currentStatus == "dormant" &&
				h.ppppProbe.running &&
				h.ppppProbe.result == nil &&
				!ppppSvcAvailable {
				h.ppppProbe.mu.Unlock()
				return
			}
			lastStatus = currentStatus
			if currentStatus == "connected" {
				lastKeepalive = now
			}
			emitStatus(currentStatus, currentServiceState)
		}
		h.ppppProbe.mu.Unlock()
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
