COMPLETION REPORT
AGENT: codex
TASK: Implement Phase 9B services (VideoQueue, FileTransferService, TimelapseService, HomeAssistantService) with tests
DATE: 2026-03-05
STATUS: done

## Files Changed
- created: internal/service/videoqueue.go
- created: internal/service/videoqueue_test.go
- created: internal/service/filetransfer.go
- created: internal/service/filetransfer_test.go
- created: internal/service/timelapse.go
- created: internal/service/timelapse_test.go
- created: internal/service/homeassistant.go
- created: internal/service/homeassistant_test.go
- created: internal/gcode/gcode.go
- created: docs/agents/reports/2026-03-05-phase9b-codex.md

## Decisions Made
- Implemented all four services as `BaseWorker`-based services to match current service lifecycle contract.
- Added explicit dependency interfaces (video controller, PPPP uploader, snapshotter, HA MQTT client, light control) to keep services testable and decoupled from not-yet-migrated concrete services.
- Implemented `VideoQueue` stall detection with restart signal (`ErrServiceRestartSignal`) and generation tracking.
- Implemented `FileTransferService` as a request-driven worker loop with progress events and mandatory GCode preprocessing (`PatchGCodeTime`, `ExtractLayerCount`).
- Implemented `TimelapseService` with in-progress persistence (`.meta`), resume window, orphan recovery, ffmpeg assembly via context-aware command execution, and pruning.
- Implemented `HomeAssistantService` with external broker connect/disconnect, discovery publishing, availability heartbeat, LWT, and bidirectional light command handling.

## Deviations from Spec
- File upload transport is implemented behind `PPPPFileUploader` interface, not hardwired to a concrete `PPPPService` implementation, because PPPP service upload API is not yet present in current Go codebase.
- Timelapse light behavior is implemented at service/session level (`off|on|auto`), while per-snapshot light sequencing is delegated to snapshot provider (`VideoQueue.CaptureSnapshot` turns light on before snapshot).

## Known Issues / TODO
- Integrate `FileTransferService` with final concrete PPPP upload path once `PPPPService` AABB upload API is wired.
- Wire `VideoQueue` live frame feed from future PPPP service event stream (`FeedFrame` currently injection-based).
- Wire `HomeAssistantService.UpdateState` calls from `MqttQueue` state machine when Phase 9 MQTT service integration is finalized.

## Open Questions for Review
- Should `TimelapseService` use fixed 10fps (as implemented) or dynamic fps formula from Python (`ceil(frame_count/30)` capped 1..30)?
- Do we want strict parity event payload shapes for websocket/API consumers now, or keep typed Go events and map them in web handlers later?

## Test Coverage
- covered: video stall restart signal, snapshot light command path, upload preprocessing/progress events, timelapse assembly + orphan recovery, HA discovery/heartbeat + light command routing
- not covered: real PPPP wire upload, real paho broker integration over network, real ffmpeg binary execution (mocked in unit tests)
