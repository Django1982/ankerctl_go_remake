# Architecture

## Package Dependency Graph

The packages form a strict layering. Never import upward.

```
cmd/ankerctl          -> everything (entry point, cobra CLI)
internal/web          -> service, model, config, notifications, gcode
internal/service      -> mqtt/client, pppp/client, model, config, notifications, gcode
internal/mqtt/client  -> mqtt/protocol, crypto, config
internal/pppp/client  -> pppp/protocol, pppp/crypto, crypto
internal/httpapi      -> crypto, config, model
internal/config       -> model
internal/model        -> (no internal deps)
internal/crypto       -> (no internal deps)
internal/pppp/crypto  -> (no internal deps)
internal/util         -> (no internal deps)
internal/gcode        -> (no internal deps)
internal/logging      -> (no internal deps)
```

## Service Framework

Services implement a lifecycle interface mirroring the Python `worker_*` pattern:

```
WorkerInit -> WorkerStart -> WorkerRun -> WorkerStop
```

The `ServiceManager` uses reference counting (borrow/return) to auto-start
services on first use and auto-stop when unused. This replaces Python's
threading-based service management with Go goroutines and channels.

## Key Design Decisions

See [decisions/](decisions/) for Architecture Decision Records (ADRs).

| Decision | Choice | Rationale |
|---|---|---|
| Language | Pure Go, no CGo | Multi-arch builds, single binary |
| HTTP Router | chi/v5 | Lightweight, stdlib-compatible |
| SQLite | modernc.org/sqlite | CGo-free, pure Go |
| WebSocket | gorilla/websocket | Mature, well-tested |
| CLI | cobra | Standard Go CLI framework |
| Logging | log/slog | Structured, stdlib |

## Frontend

The frontend (HTML/JS/Cash.js) remains unchanged from the Python project.
Templates are converted from Jinja2 to Go `html/template` syntax and served
via `//go:embed`.
