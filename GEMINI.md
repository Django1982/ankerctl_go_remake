# GEMINI.md - ankerctl (Go) Instructional Context

## Project Overview
This project is a Go reimplementation of [ankerctl](https://github.com/ankermake/ankermake-m5-protocol), originally written in Python. It serves as a CLI and web UI tool for monitoring, controlling, and interfacing with AnkerMake M5 3D printers. The primary goals are improved security, performance, easier deployment, and native concurrency.

### Core Technologies
- **Language:** Go 1.24
- **HTTP Framework:** `github.com/go-chi/chi/v5`
- **MQTT Client:** `github.com/eclipse/paho.mqtt.golang`
- **WebSockets:** `github.com/gorilla/websocket`
- **Database:** `modernc.org/sqlite` (pure Go, CGO-free)
- **CLI Framework:** `github.com/spf13/cobra`
- **Logging:** Structured logging via `log/slog`

## Architecture & Design Patterns

### Package Layering (Strict)
Never introduce upward imports. Follow this hierarchy:
1. `cmd/ankerctl` (Entry point, CLI)
2. `internal/web` (HTTP handlers, middleware, templates, WebSockets)
3. `internal/service` (Background services: MqttQueue, PPPPService, VideoQueue, etc.)
4. `internal/mqtt/client`, `internal/pppp/client` (Protocol clients)
5. `internal/httpapi` (Anker Cloud API)
6. `internal/config`, `internal/model`, `internal/crypto`, `internal/pppp/crypto` (Foundational logic)
7. `internal/util`, `internal/logging`, `internal/gcode` (Shared utilities)

### Service Lifecycle
Background operations are managed by a `ServiceManager` (`internal/service/manager.go`).
- **Interface:** Services implement `WorkerInit`, `WorkerStart`, `WorkerRun`, and `WorkerStop`.
- **Reference Counting:** The manager use borrow/return semantics to auto-start/stop services based on usage.
- **State Machine:** services transition through Starting -> Running -> Stopping -> Stopped.

### Protocols
- **MQTT (Anker Broker):** Port 8789. Uses a 63-byte fixed header + AES-256-CBC encrypted body + XOR checksum. IV is fixed: `"3DPrintAnkerMake"`.
- **PPPP (UDP P2P):** Port 32108 (LAN). 8 logical channels over one UDP socket. Complexities include DRW pipelining (64-packet window, 0.5s timeout) and `CyclicU16` wraparound sequencing.

## Building and Running

### Common Commands
- **Build:** `go build -o ankerctl ./cmd/ankerctl/`
- **Test (All):** `go test ./...`
- **Test (Package):** `go test ./internal/crypto/...`
- **Run Server:** `./ankerctl webserver run`
- **Vet:** `go vet ./...`

## Development Conventions

### Security Mandates (CRITICAL)
- **Secrets:** NEVER log `auth_token`, `mqtt_key`, or `api_key`. Use redaction in logging.
- **Input Validation:** Parameterized SQL queries ONLY. No string concatenation for DB access.
- **Filesystem:** Configuration directory must be `0700`.
- **Production Integrity:** Never use `panic()` on production paths; always return and propagate errors.
- **Context:** All goroutines must respect `context.Context` for graceful shutdown.

### Coding Style
- **Naming:** `PascalCase` for exported, `camelCase` for unexported. Package names must be lowercase.
- **Formatting:** `gofmt` is mandatory.
- **Testing:** Table-driven tests are preferred, especially for protocol parsing and crypto logic.

### Git Workflow & Branching (MANDATORY)
To ensure project stability and a clean history:
1. **Branching:** Never work directly on the `main` branch. Create a feature branch for every task: `git checkout -b <branch-name>`.
2. **Atomic Commits:** Each commit must represent exactly ONE logical change or sub-task. Use clear, imperative commit messages (e.g., `feat(httpapi): implement ECDH login`).
3. **Merging:** Only merge back into `main` after all tests (`go test ./...`) pass and implementation is verified.

### Migration Status
Refer to `MIGRATION_PLAN.md` for the full 16-phase roadmap. 
- **Completed:** Phase 1-4, 6, 8, 9, 11, 12.
- **In Progress:** Phase 5 (SQLite fine-tuning), Phase 7 (PPPP client refinements), Phase 10 (Remaining HTTP handlers).
- **Recent Reports:** Check `docs/agents/reports/` for detailed phase-specific status and review notes.

## Reference Documentation
- `README.md`: General project info.
- `CLAUDE.md`: Implementation-specific guidance and protocol constants.
- `MIGRATION_PLAN.md`: Full roadmap and Python-to-Go mapping.
- `docs/architecture/`: Detailed architecture documentation.
