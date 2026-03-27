# Development Guide

## Prerequisites

- Go 1.22 or later
- `ffmpeg` (for timelapse tests)
- `git`

## Getting Started

```sh
git clone https://github.com/Django1982/ankerctl_go_remake.git
cd ankerctl_go_remake

# Install pre-commit hooks (blocks direct commits to main)
sh scripts/install-hooks.sh

# Download vendor assets (Bootstrap, Chart.js etc.) — REQUIRED before first build
# Linux/macOS:
bash scripts/prepare-web-vendor.sh
# Windows (PowerShell):
# .\scripts\prepare-web-vendor.ps1

# Build
go build -o ankerctl ./cmd/ankerctl/

# Run tests
go test ./...

# Vet
go vet ./...
```

> **Why `prepare-web-vendor.sh`?** The binary embeds all frontend assets via `//go:embed static/*`. The vendor libraries (Bootstrap, Chart.js, jMuxer, Cash.js) are not checked into git but downloaded from CDN by this script. Skipping it results in a blank web UI. Run it once after cloning, and again after pulling if `scripts/prepare-web-vendor.sh` changed.

## Git Workflow

1. **Never commit directly to `main`.** The pre-commit hook blocks it.
2. Create a feature branch: `git checkout -b feat/my-feature`
3. Commit often with atomic, imperative messages: `fix(mqtt): redact secrets in logs`
4. Run tests before merging: `go test ./...`
5. Merge into `main` only after tests pass.

### Commit Message Convention

```
type(scope): subject

Types: feat, fix, refactor, test, docs, chore, perf
Scope: package name (mqtt, pppp, web, config, crypto, etc.)
```

## Architecture

### Package Dependency Order

Packages form a strict layering. Never import upward.

```
cmd/ankerctl          --> everything (entry point)
internal/web          --> service, model, config, notifications, gcode
internal/service      --> mqtt/client, pppp/client, model, config, notifications, gcode
internal/mqtt/client  --> mqtt/protocol, crypto, config
internal/pppp/client  --> pppp/protocol, pppp/crypto, crypto
internal/httpapi      --> crypto, config, model
internal/config       --> model
internal/model        --> (no internal deps)
internal/crypto       --> (no internal deps)
internal/pppp/crypto  --> (no internal deps)
internal/util         --> (no internal deps)
internal/gcode        --> (no internal deps)
internal/logging      --> (no internal deps)
```

### Key Design Decisions

| Decision | Rationale |
|----------|-----------|
| Pure Go (no CGo) | Cross-compilation, single static binary |
| chi/v5 | Lightweight, stdlib-compatible HTTP router |
| gorilla/websocket | De-facto standard for WebSocket in Go |
| modernc.org/sqlite | CGO-free SQLite for multi-arch builds |
| log/slog | Structured logging, stdlib since Go 1.21 |
| Custom HMAC-SHA256 sessions | No gorilla/sessions dependency |

### Service Framework

Services implement a lifecycle interface with four phases:

```
WorkerInit --> WorkerStart --> WorkerRun (loop) --> WorkerStop
```

The `ServiceManager` uses reference counting:
- `Borrow(name)` increments the ref count, auto-starts on first borrow
- `Return(name)` decrements; service stops when count reaches zero
- `VideoQueue` exception: stays running when video is enabled

Services communicate via the `Notify`/`Tap` pattern:
- `Notify(data)` broadcasts to all tapped handlers
- `Tap(handler)` registers a handler, returns a cleanup function

### Concurrency Patterns

| Python | Go Equivalent |
|--------|---------------|
| `threading.Thread` + `worker_*()` | goroutine + `for`-loop with `select` |
| `Queue` | `chan interface{}` |
| `threading.Lock` | `sync.Mutex` |
| Context manager (borrow/put) | `defer sm.Return(name)` after `sm.Borrow(name)` |
| `ServiceRestartSignal` | Sentinel error causing goroutine restart |

### Frontend

The frontend (HTML/JS/CSS) is unchanged from the Python original. Templates are converted from Jinja2 to Go `html/template` syntax. Static files are embedded via `//go:embed static/*`.

## Testing

### Running Tests

```sh
# All tests
go test ./...

# With race detector
go test -race ./...

# Specific package
go test ./internal/crypto/...

# Single test
go test -run TestAESEncryptDecrypt ./internal/crypto/

# Verbose
go test -v ./internal/mqtt/protocol/...
```

### Test Conventions

- Use table-driven tests for protocol and crypto code
- All protocol/crypto logic must have tests (zero-tolerance policy)
- Test files go next to the code: `foo.go` and `foo_test.go`
- Use `testdata/` directories for fixtures

### Example: Table-Driven Test

```go
func TestProgressScale(t *testing.T) {
    tests := []struct {
        name     string
        mqtt     int
        expected int
    }{
        {"zero", 0, 0},
        {"half", 5000, 50},
        {"full", 10000, 100},
        {"quarter", 2500, 25},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := tt.mqtt / 100
            if got != tt.expected {
                t.Errorf("got %d, want %d", got, tt.expected)
            }
        })
    }
}
```

## Security Requirements

- **Never log** `auth_token`, `mqtt_key`, or `api_key`. Use `logging.Redact()`.
- **Parameterized SQL** queries everywhere (no string concatenation).
- **Strip HTML tags** from filament text fields (XSS prevention).
- **Validate path parameters** for timelapse download and log viewer (path traversal).
- **Config directory:** `os.Chmod(dir, 0700)`.
- **No panics** on production paths -- always propagate errors.
- **All goroutines** must respect `context.Context` for clean shutdown.

## Docker Development

```sh
# Build locally
docker build -t ankerctl:dev .

# Run with local config
docker run --network host -v ~/.ankerctl:/root/.ankerctl ankerctl:dev

# Build with version tag
docker build --build-arg VERSION=v0.9.0 -t ankerctl:v0.9.0 .
```

## Migration Plan

See [MIGRATION_PLAN.md](https://github.com/Django1982/ankerctl_go_remake/blob/main/MIGRATION_PLAN.md) for the full 16-phase migration roadmap from Python to Go.

## Python Source Reference

When implementing or debugging, cross-reference the Python original:

| Go Package | Python Source |
|------------|--------------|
| `internal/mqtt/protocol` | `libflagship/mqtt.py`, `libflagship/amtypes.py` |
| `internal/mqtt/client` | `libflagship/mqttapi.py` |
| `internal/pppp/protocol` | `libflagship/pppp.py`, `libflagship/cyclic.py` |
| `internal/pppp/client` | `libflagship/ppppapi.py` |
| `internal/pppp/crypto` | `libflagship/megajank.py` (PPPP section) |
| `internal/crypto` | `libflagship/megajank.py` (AES/ECDH section) |
| `internal/httpapi` | `libflagship/httpapi.py`, `libflagship/seccode.py` |
| `internal/config` | `cli/config.py`, `libflagship/logincache.py` |
| `internal/model` | `cli/model.py` |
| `internal/service` | `web/lib/service.py`, `web/service/*.py` |
| `internal/web` | `web/__init__.py` |
