// Package service implements the service framework and all background
// services for the ankerctl web application.
//
// The framework provides a Service interface with lifecycle methods
// (WorkerInit, WorkerStart, WorkerRun, WorkerStop) and a ServiceManager
// with reference-counted borrowing for safe concurrent access.
//
// Services:
//   - MqttQueue:          Core MQTT message processing and state machine
//   - PPPPService:        LAN P2P connection management
//   - VideoQueue:         H.264 video streaming from printer camera
//   - FileTransferService: GCode upload pipeline via PPPP
//   - PrintHistory:       SQLite print log (not a Service, accessed via MqttQueue)
//   - TimelapseService:   Periodic snapshots assembled into MP4 video
//   - HomeAssistantService: External MQTT broker for HA Discovery
//   - FilamentStore:      SQLite filament profiles (not a Service)
//
// Python sources: web/lib/service.py, web/service/*.py
package service
