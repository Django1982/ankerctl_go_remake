// Package ws implements WebSocket endpoint handlers for the ankerctl
// web application.
//
// Endpoints:
//   - /ws/mqtt:       Server->Client, raw MQTT message JSON stream
//   - /ws/video:      Server->Client, raw H.264 video frame stream
//   - /ws/pppp-state: Server->Client, PPPP connection status
//   - /ws/upload:     Server->Client, file upload progress events
//   - /ws/ctrl:       Bidirectional, light/video control (inline auth)
//
// Each WebSocket handler uses the ServiceManager.Stream() pattern to
// receive data from a service via channels, with 1-second timeouts
// to detect service stops.
//
// Python source: web/__init__.py (WebSocket route functions)
package ws
