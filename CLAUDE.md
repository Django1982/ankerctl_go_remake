# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Go reimplementation of [ankerctl](https://github.com/ankermake/ankermake-m5-protocol) — a CLI and web UI for AnkerMake M5 3D printers. The Python source lives at `/data_hdd/ankermake-m5-protocol-django1982/`. This Go rewrite targets 1:1 feature parity.

**Module**: `github.com/django1982/ankerctl` (Go 1.22)
**Migration plan**: `MIGRATION_PLAN.md` — 16-phase roadmap, implementation has progressed well beyond scaffold (see `docs/agents/reports/` for recent phase reports/reviews).

## Common Commands

```bash
# Build
go build -o ankerctl ./cmd/ankerctl/

# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/crypto/...

# Single test
go test -run TestFunctionName ./internal/crypto/

# Run the server (once implemented)
./ankerctl webserver run

# Vet + lint
go vet ./...
```

## Mandates (CRITICAL)

- **Zero-Tolerance-Policy:** No unverified/untested code in `main`. All protocol/crypto logic MUST have table-driven tests.
- **Peer-Review-Pflicht:** Feature branches MUST be cross-checked by another agent (Gemini CLI) for compliance (layering, security, bit-exactness) before merging.
- **Security:** Never log `auth_token`, `mqtt_key`, or `api_key`. Use `logging.Redact()` for payloads.
- **Layering:** Never import upward across internal package layers (see Architecture).
- **Automation:** Prefer non-interactive commands (e.g., `git merge --no-edit`, `npm --yes`) to avoid hangs.

## Git Workflow (MANDATORY)

To ensure a stable `main` branch and professional history:
1. **Branching:** Create a new branch for every task: `git checkout -b <branch-name>`.
2. **Atomic Commits:** Commit often, but only one logical change per commit.
3. **Merging:** Only merge into `main` after verifying the implementation with tests.
4. **Messages:** Use imperative, concise commit subjects (e.g., `fix(mqtt): redact secrets in logs`).

**Hook enforcement:** After cloning, run `sh scripts/install-hooks.sh` once. This installs a `pre-commit` hook that hard-blocks direct commits to `main`.

## Architecture

### Package Dependency Order

The packages form a strict layering. Never import upward:

```
cmd/ankerctl          → everything (entry point, cobra CLI)
internal/web          → service, model, config, notifications, gcode
internal/service      → mqtt/client, pppp/client, model, config, notifications, gcode
internal/mqtt/client  → mqtt/protocol, crypto, config
internal/pppp/client  → pppp/protocol, pppp/crypto, crypto
internal/httpapi      → crypto, config, model
internal/config       → model
internal/model        → (no internal deps)
internal/crypto       → (no internal deps)
internal/pppp/crypto  → (no internal deps)
internal/util         → (no internal deps)
internal/gcode        → (no internal deps)
internal/logging      → (no internal deps)
```

### Key Protocol Details

**MQTT** (`internal/mqtt/`): Connects to Anker's broker on port 8789. Each message is a 63-byte fixed header + AES-256-CBC encrypted body + XOR checksum. The AES IV is the fixed string `"3DPrintAnkerMake"`. Topics follow the pattern `/phone/maker/{SN}/notice` (subscribe) and `/device/maker/{SN}/command` (publish).

**PPPP** (`internal/pppp/`): UDP-based P2P protocol for LAN communication. One UDP socket serves 8 logical channels. LAN discovery on port 32108. Key complexity: DRW pipelining with a 64-packet in-flight window and 0.5s retransmission timeout. CyclicU16 is a 16-bit wraparound counter used for sequencing.

**Crypto** (`internal/crypto/`): AES-256-CBC with PKCS7 padding. ECDH uses secp256r1 with Anker's hardcoded public key (X/Y in `MIGRATION_PLAN.md`). The PPPP `crypto_curse/decurse` functions live in `internal/pppp/crypto/` and use shuffle tables — these must be bit-exact.

### Service Framework (`internal/service/`)

Services implement a lifecycle interface: `WorkerInit → WorkerStart → WorkerRun → WorkerStop`. The `ServiceManager` uses reference counting (borrow/return) to auto-start services on first use and auto-stop when unused. Core services:

- **MqttQueue**: The heart of the app. Handles 39 message types (ct values). The state machine on `ct=1000`: 0=idle, 1=printing, 2=paused, 8=aborted. Progress arrives as 0–10000 from MQTT; divide by 100 for the API.
- **PPPPService**: LAN connection via PPPP. Restarts on `ConnectionReset`.
- **VideoQueue**: H.264 stream from the printer camera. Stall detection at 15s. Profiles: sd (848×480), hd (1280×720), fhd (1920×1080, snapshot-only).
- **FileTransferService**: GCode patching + upload via PPPP channels.
- **TimelapseService**: ffmpeg-based. Resume window is 60 min; orphan recovery up to 24h.
- **HomeAssistantService**: Publishes HA MQTT Discovery payloads (11 sensors, 2 binary, 1 switch, 1 camera) to an external broker with 60s heartbeat.

### Web Layer (`internal/web/`)

HTTP server using chi/v5. Default listen: `127.0.0.1:4470`.

**Auth rules** (must match Python exactly):
- POST/DELETE: always require API key
- Protected GET paths: `/api/ankerctl/server/reload`, `/api/debug/*`, `/api/settings/mqtt`, `/api/notifications/settings`
- `/api/ankerctl/config/upload` and `/api/ankerctl/config/login` are exempt when no printer is configured

**Middleware order** (outer to inner): recovery → request-id → access-logging → security-headers → rate-limit → body-size-limit → require-printer → block-unsupported-device → auth

**WebSockets**: `/ws/mqtt` (MQTT events), `/ws/video` (binary H.264), `/ws/pppp-state` (connection poll), `/ws/upload` (file transfer progress), `/ws/ctrl` (bidirectional, has inline auth).

### Config and Models (`internal/config/`, `internal/model/`)

Config is JSON on disk. The Python source uses `__type__` for polymorphic deserialization — Go must handle this with custom `UnmarshalJSON`. Config directory must be chmod 0700. Auth tokens and MQTT keys must never appear in logs.

### Frontend (`static/`)

Unchanged from Python. HTML templates are converted from Jinja2 to `html/template` syntax. Served as embedded files via `//go:embed static/*`.

## Environment Variables

All must be supported identically to Python. Key ones:

| Variable | Purpose |
|---|---|
| `FLASK_HOST` / `FLASK_PORT` | Bind address (rename target: `ANKERCTL_HOST`/`ANKERCTL_PORT`) |
| `ANKERCTL_API_KEY` | API key (min 16 chars, `[a-zA-Z0-9_-]`) |
| `ANKERCTL_DEV_MODE` | Enables `/api/debug/*` endpoints |
| `PRINTER_INDEX` | Active printer index |
| `UPLOAD_MAX_MB` | Upload size limit |
| `TIMELAPSE_ENABLED`, `TIMELAPSE_INTERVAL_SEC` | Timelapse control |
| `HA_MQTT_*` | Home Assistant MQTT bridge settings |
| `APPRISE_*` | Notification settings |

See `MIGRATION_PLAN.md` for the full list.

## Critical Constants

These are protocol-level and must be bit-exact:

```go
const MQTTAesIV   = "3DPrintAnkerMake" // Fixed 16-byte AES IV
const MQTTPort    = 8789
const PPPPLanPort = 32108
const PPPPSeed    = "EUPRAKM"
const DefaultHost = "127.0.0.1"
const DefaultPort = 4470
```

## Python Source Reference

When implementing a package, cross-reference the Python original:

| Go Package | Python Source |
|---|---|
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
| `internal/web` | `web/__init__.py` (~1700 LOC, 40+ routes) |
| `internal/notifications` | `libflagship/notifications/`, `web/notifications.py` |
| `internal/gcode` | `cli/util.py` (gcode functions) |

## Security Requirements

- Parameterized SQL queries everywhere (never string concatenation)
- Strip HTML tags from filament text fields (XSS prevention)
- Validate path parameters for timelapse download and log viewer (path traversal)
- Config dir: `os.Chmod(dir, 0700)`
- Redact `auth_token`, `mqtt_key`, `api_key` from all log output
- No panics on production paths — always propagate errors
- All goroutines must respect `context.Context` for clean shutdown
