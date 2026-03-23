package handler

import "net/http"

// ShutdownTrigger is implemented by the Server to allow an HTTP handler to
// initiate a graceful process shutdown without importing the web package
// (which would create a circular dependency).
type ShutdownTrigger interface {
	TriggerShutdown()
}

// ServerShutdown responds with 200 OK and then signals the process to shut
// down gracefully via the ShutdownTrigger. The response is sent before the
// shutdown signal so that the client receives a clean reply.
//
// Route: POST /api/ankerctl/server/shutdown
// Auth: POST routes require API key (enforced by middleware).
func (h *Handler) ServerShutdown(w http.ResponseWriter, _ *http.Request) {
	h.writeJSON(w, http.StatusOK, map[string]string{"message": "Server is shutting down..."})

	if h.shutdownTrigger != nil {
		// Fire in a separate goroutine so the HTTP response is fully flushed
		// before the server begins shutting down.
		go h.shutdownTrigger.TriggerShutdown()
	}
}
