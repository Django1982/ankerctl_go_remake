// Package web implements the HTTP server, REST API routes, WebSocket
// endpoints, and middleware stack for the ankerctl web application.
//
// It serves 40+ REST endpoints, 5 WebSocket streams, and the static
// frontend files (HTML/JS/CSS). Authentication is enforced via API key
// middleware with configurable rules per HTTP method and path.
//
// Key files:
//   - server.go:    HTTP server setup and lifecycle
//   - routes.go:    All REST endpoint handlers
//   - websocket.go: WebSocket stream handlers
//   - auth.go:      API key authentication middleware
//   - templates.go: Go html/template rendering (replaces Jinja2)
//
// Python source: web/__init__.py
package web
