COMPLETION REPORT
AGENT: codex
TASK: Implement Phase 8 service framework (service interface, base worker, service manager, tests)
DATE: 2026-03-05
STATUS: done

## Files Changed
- created: internal/service/service.go
- created: internal/service/worker.go
- created: internal/service/manager.go
- created: internal/service/service_test.go
- created: internal/service/manager_test.go
- created: internal/service/worker_test.go
- created: docs/agents/reports/2026-03-05-phase8-codex.md

## Decisions Made
- Implemented `RunState` and `Service` interface exactly as requested in `internal/service/service.go`.
- Added typed sentinel `ServiceRestartSignal` plus `ErrServiceRestartSignal` and `IsServiceRestartSignal()` to support restart-trigger behavior.
- Implemented `BaseWorker` as a goroutine-driven lifecycle loop with state transitions: `Stopped -> Starting -> Running -> Stopping`.
- Added `BindHooks()` in `BaseWorker` so embedded services can route lifecycle callbacks (`WorkerInit/Start/Run/Stop`) to the outer service implementation.
- Implemented `Tap/Notify` with thread-safe handler map and unsubscribe closure (`sync.Once` protected).
- Implemented restart holdoff (1s) for explicit `Restart()` and `ServiceRestartSignal` from `WorkerRun`.
- Implemented `ServiceManager` with ref-counted `Borrow`/`Return`, auto-start on `0->1`, auto-stop on `1->0`, and reverse-order `Shutdown()`.
- Added `videoqueue` exception handling in `Return()`; supports `VideoEnabled() bool` method and reflected bool fields (`video_enabled`, `videoEnabled`, `VideoEnabled`).

## Deviations from Spec
- `ServiceManager.Register(svc Service)` ignores duplicate registrations instead of panicking or returning an error, because signature has no error return and runtime panic is undesirable in production paths.
- `ServiceManager.Shutdown()` currently calls `Stop()` for all services in reverse order; it does not block for full worker termination because `Service` interface does not expose a wait/join method.

## Known Issues / TODO
- If a future service embeds `BaseWorker`, it should call `BindHooks(serviceInstance)` in its constructor to activate outer lifecycle hooks.

## Open Questions for Review
- Should duplicate `Register()` be treated as no-op (current) or should we extend API later to return an error?
- Should `Service` grow a blocking shutdown/wait API in later phases to make `ServiceManager.Shutdown()` fully synchronous like Python's `await_stopped()` behavior?

## Test Coverage
- covered: service interface compatibility, restart-signal detection, lifecycle state transitions (start/stop), tap/notify unsubscribe behavior, restart holdoff timing, manager borrow/return ref-counting, unknown borrow error, videoqueue no-auto-stop exception, reverse shutdown order
- not covered: integration with concrete services (`mqttqueue`, `pppp`, `videoqueue`) because those implementations are not yet migrated in this phase
