REVIEW REPORT
REVIEWER: go-migration-architect
PHASE: 8 (Service Framework)
DATE: 2026-03-05

## Findings

### CRITICAL

**C1: ServiceManager.Shutdown() — goroutine leak (FIXED)**

`ServiceManager.Shutdown()` called `svc.Stop()` and returned immediately. `Stop()` only cancels
`runCancel` (the per-WorkerRun context) and sets `wanted=false`; it does NOT cancel `loopCancel`.
The `runLoop` goroutine would transition to `StateStopped` and then park in `waitLoopSignal`,
sleeping on a 50 ms ticker forever. The goroutine was never reaped.

Python's `ServiceManager.atexit()` uses a three-pass shutdown:
1. `svc.stop()` — signal
2. `svc.await_stopped()` — spin-wait until `RunState.Stopped`
3. `svc.shutdown()` — `running=False` + `Thread.join()` — full goroutine join

The Go `BaseWorker.Shutdown()` correctly mirrors this: it calls `loopCancel()` and then
blocks on `<-loopDone`. But `ServiceManager.Shutdown()` was not calling `svc.Shutdown()`.

Fix applied:
- `ServiceManager.Shutdown()` now performs a two-pass shutdown: signal all services with
  `svc.Stop()` concurrently (matching Python's stop-all-first approach), then block on each
  `svc.Shutdown()` in reverse order.
- Added `Shutdown()` to the `Service` interface so the manager can invoke it without a type
  assertion.
- Added `TestManagerShutdownBlocks` using real `BaseWorker`-backed workers to prove the
  goroutine exits within 5 seconds.
- `mockService.Shutdown()` added (no-op) to satisfy the updated interface.

**C2: initialized field read outside mutex (FIXED)**

`runLoop()` read `w.initialized` on line 206 without holding `w.mu`, while the write on
line 209 did hold `w.mu`. Although only one `runLoop` goroutine runs at a time (enforced by
the `loopDone != nil` guard in `Start()`), the race detector could flag the unguarded read
if any other code path accessed `initialized` under the lock in the same window.

Fix applied: the read is now protected:
```go
w.mu.Lock()
needInit := !w.initialized
w.mu.Unlock()
```

### MEDIUM

**M1: WorkerRun spin loop — no throttle if nil returned immediately**

Python's `worker_run(timeout=0.1)` signature enforces a 100 ms minimum iteration time.
The Go equivalent has no such floor: if a `WorkerRun` implementation returns `nil`
immediately (e.g., buffer empty, no event ready), the `StateRunning` case in `runLoop`
loops back and calls `WorkerRun` again without yielding. The 50 ms ticker is only active
in the `StateStopped`/`StateStarting` wait paths; in `StateRunning` there is no ticker
guard, so a naive implementation burns 100% CPU.

The `BaseWorker.WorkerRun` default correctly blocks on `ctx.Done()`. The new GoDoc
on `Service.WorkerRun` and the internal `workerHooks.WorkerRun` now document the blocking
contract explicitly. No framework-level change was applied here — throttling is a contract
on the implementor, consistent with the Go style of trusting the caller. This must be
audited for each concrete service implementation (MqttQueue, PPPPService, etc.) when those
are migrated in later phases.

**M2: BindHooks — undocumented ordering requirement**

`BindHooks(self)` must be called before `Start()`. If an embedding service forgets,
`WorkerInit/Start/Run/Stop` dispatch to the `BaseWorker` no-ops rather than the concrete
implementation. There is no compile-time or runtime enforcement; the symptom is a silently
non-functional service.

Fix applied: `BindHooks` GoDoc now states: "MUST call BindHooks(self) in constructor,
before Start() is called."

### LOW / OK

**L1: BindHooks pattern — is it idiomatic Go?**

The pattern exists because Go embedding does not have virtual dispatch: `BaseWorker.runLoop()`
calling `w.WorkerStart()` resolves to `BaseWorker.WorkerStart()` (the no-op), not the
embedding struct's override. `BindHooks` stores the outer struct as a `workerHooks` interface,
routing calls through the interface dispatch table instead. This is the standard Go solution
to this problem (sometimes called the "self-referential interface" or "delegate" pattern).

Alternative: function fields (`onStart func() error` etc.) are more explicit but harder to
document and less discoverable. Interface fields require more boilerplate per service.
`BindHooks` is the right tradeoff for this codebase. Verdict: acceptable and well-executed.

**L2: Reflection in keepVideoQueueRunning — acceptable tradeoff**

`keepVideoQueueRunning` first tries a `VideoEnabled() bool` interface assertion; the
reflection fallback (field name iteration) only fires if the interface is absent. The
interface path is the fast, idiomatic path. The reflection path exists for forward
compatibility during migration. All tests pass. Low risk.

**L3: Tap/Notify — no deadlock possible (CONFIRMED OK)**

`Notify()` copies the handler slice under `handlersMu.RLock()`, releases the lock, then
calls handlers. Handlers calling `unsub()` (which calls `handlersMu.Lock()`) will not
deadlock because no read lock is held during the call. Verified by code inspection.

**L4: Restart() holdoff — matches Python 1 s (CONFIRMED OK)**

`restartHoldoff = time.Second` is set at package level. `TestWorkerRestartHoldoff` measures
delta >= 900 ms between first and second `WorkerStart()` call. Python uses `delay=1` seconds
in `Holdoff.reset()`. Correct.

**L5: ServiceManager duplicate Register — no-op vs panic**

Codex chose a silent no-op for duplicate `Register()` calls. Python's `ServiceManager.register()`
raises `KeyError`. The no-op avoids a production panic, which is consistent with the project's
"no panics on production paths" rule (CLAUDE.md). Acceptable deviation; callers that need
strict uniqueness should check `Contains()` themselves.

**L6: ServiceRestartSignal — errors.As with struct value (CONFIRMED OK)**

`errors.As(err, &sig)` with `sig ServiceRestartSignal` (non-pointer) requires the target type
to implement `error`. `ServiceRestartSignal` has an `Error() string` method. `errors.As` on a
value type works because `errors.As` uses reflection to match assignable types, and the wrapped
error IS a `ServiceRestartSignal`. Test `TestIsServiceRestartSignal` covers the wrapped case.

## Fixes Applied

| # | File | Change |
|---|------|--------|
| 1 | `internal/service/service.go` | Added `Shutdown()` to `Service` interface with GoDoc |
| 2 | `internal/service/service.go` | Added GoDoc to `WorkerRun` explaining blocking contract |
| 3 | `internal/service/manager.go` | `Shutdown()` now two-pass: Stop-all then Shutdown-all (blocking) |
| 4 | `internal/service/worker.go` | `initialized` read now protected by `w.mu.Lock()` |
| 5 | `internal/service/worker.go` | `BindHooks` GoDoc: must call before `Start()` |
| 6 | `internal/service/worker.go` | `workerHooks.WorkerRun` GoDoc: blocking contract documented |
| 7 | `internal/service/manager_test.go` | `mockService.Shutdown()` stub added |
| 8 | `internal/service/manager_test.go` | `TestManagerShutdownBlocks` added (real BaseWorker goroutines) |
| 9 | `internal/service/manager_test.go` | `"time"` import added |

## Python Compliance

- [x] Shutdown blocks until workers stop — `ServiceManager.Shutdown()` now calls `Shutdown()` per
      service; `BaseWorker.Shutdown()` waits on `<-loopDone`
- [x] Restart holdoff matches Python (1s) — `restartHoldoff = time.Second` verified by test
- [x] Tap/Notify fan-out matches Python event pattern — handler slice iteration, no lock during call
- [x] ServiceRestartSignal triggers clean restart — tested via `TestWorkerRestartHoldoff` and
      `TestIsServiceRestartSignal`
- [x] VideoQueue no-auto-stop exception correct — `VideoEnabled() bool` interface preferred over
      reflection; test `TestManagerVideoQueueException` passes

## Test Results

```
go test -race -count=1 ./internal/service/...
ok  github.com/django1982/ankerctl/internal/service  2.167s
```

10 tests, 0 failures, race-clean. `go vet` clean.

## Verdict

APPROVED WITH FIXES

The framework logic is solid. The critical goroutine leak (C1) and initialized-field data
race (C2) have been fixed. The WorkerRun spin-loop risk (M1) is a contract issue, not a
framework bug; it must be reviewed for each concrete service implementation. The BindHooks
pattern is idiomatic and correctly solves the Go virtual-dispatch problem.
