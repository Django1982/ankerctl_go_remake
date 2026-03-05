COMPLETION REPORT
AGENT: codex
TASK: Implement Phase 9A MqttQueue and PPPPService with core lifecycle, dispatch logic, and tests
DATE: 2026-03-05
STATUS: done

## Files Changed
- created: internal/service/mqttqueue.go
- created: internal/service/mqttqueue_test.go
- created: internal/service/pppp.go
- created: internal/service/pppp_test.go
- created: docs/agents/reports/2026-03-05-phase9a-codex.md

## Decisions Made
- Implemented `MqttQueue` with injected MQTT client factory to keep lifecycle testable and decouple construction from transport details.
- Implemented deferred history start (`ct=1044` filename + `ct=1000 value=1`) using `internal/db.DB.RecordStart` as required.
- Normalized progress values in one dedicated helper (`normalizeProgress`) to guarantee 0-100 output for MQTT values that arrive as 0-10000.
- Forwarded relevant MQTT events (`ct=1000`, `ct=1001`, `ct=1044`, payloads with progress) to HomeAssistant/Timelapse sinks through `Notify`.
- Implemented `PPPPService` as blocking `WorkerRun` with `client.Run(ctx)` in a goroutine plus periodic XZYH drain over all 8 channels.
- Implemented PPPP restart behavior by returning `ErrServiceRestartSignal` on any PPPP run-loop error or disconnected state.

## Deviations from Spec
- Python reference path `web/service/mqttqueue.py` does not exist; implementation was based on `web/service/mqtt.py` (actual source in the Python repo).
- `MqttQueue` disconnect detection is based on MQTT query/publish failures and client lifecycle errors available in current Go abstractions; the existing `internal/mqtt/client` API does not expose explicit transport disconnect callbacks yet.

## Known Issues / TODO
- `PPPPService` currently dispatches XZYH payload bytes only (`func([]byte)`), not full `Xzyh` metadata; this matches task signature but may need extension later for command-aware handlers.
- `MqttQueue` currently implements required start/deferred-history/control behavior but not the full advanced notification/history-finish/failure logic from Python (out of scope for this phase task).

## Open Questions for Review
- Should `MqttQueue` emit additional structured service-level events beyond current `print_state` map for downstream WebSocket consumers?
- Should PPPP handler registration be widened to include command/channel metadata in a future phase?

## Test Coverage
- covered: `ct=1000` start + `ct=1044` deferred filename/history start; progress normalization helper; PPPP restart-on-error behavior.
- not covered: live MQTT broker connectivity, PPPP LAN discovery against real device/network, full end-to-end HA/Timelapse integrations.
