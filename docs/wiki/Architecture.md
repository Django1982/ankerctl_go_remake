# Architecture

This page describes the internal architecture of the ankerctl Go rewrite.

**Module:** `github.com/django1982/ankerctl`
**Go version:** 1.22+
**Key libraries:** chi/v5 (HTTP), gorilla/websocket, modernc.org/sqlite (CGO-free), paho.mqtt.golang, cobra (CLI)

---

## Project Structure

```
cmd/ankerctl/              CLI entry point (cobra)
internal/
  config/                  Configuration management (JSON on disk)
  crypto/                  AES-256-CBC, ECDH, XOR checksums
  db/                      SQLite database layer
  mqtt/
    protocol/              MQTT message types, packet structures
    client/                MQTT client (Anker broker connection)
  pppp/
    protocol/              PPPP packet types (UDP P2P)
    client/                PPPP API, channels, file transfer
    crypto/                PPPP-specific crypto (curse/decurse)
  httpapi/                 Anker Cloud HTTP API client
  service/                 Service framework + all background services
  web/
    server.go              HTTP server setup, middleware stack
    routes.go              All REST endpoints
    handler/               Grouped HTTP handler functions
    ws/                    WebSocket endpoint handlers
    middleware/            Auth, security headers, rate limiting
    templates.go           Go html/template rendering
  notifications/           Apprise notification client
  gcode/                   GCode parsing (time patching, layer count)
  model/                   Data models (Config, Account, Printer)
  util/                    Shared utilities
  logging/                 Structured logging (log/slog)
static/                    Frontend files (HTML/JS/CSS, unchanged from Python)
```

---

## Package Dependency Order

Packages form a strict layering. **Never import upward.**

```
cmd/ankerctl          -> everything (entry point)
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

> Violation of this layering (importing a package from a higher level) will break the architecture and must be rejected in code review.

---

## Service Framework

All background services implement a lifecycle interface and are managed by the `ServiceManager`.

### Lifecycle

```
WorkerInit -> WorkerStart -> WorkerRun -> WorkerStop
```

| Method | Purpose |
|--------|---------|
| `WorkerInit()` | One-time initialization (called once) |
| `WorkerStart()` | Start resources (connections, goroutines) |
| `WorkerRun()` | Main loop (blocking, runs until stop signal) |
| `WorkerStop()` | Release resources, close connections |

### State Machine

```
Starting -> Running -> Stopping -> Stopped
```

- **Holdoff:** 1-second cooldown between restarts
- **ServiceRestartSignal:** Sentinel error that triggers a clean restart

### ServiceManager

The `ServiceManager` uses **reference counting** (borrow/return) to auto-start services on first use and auto-stop when unused.

- `Borrow(name)` -- increment refcount, auto-start if needed
- `Return(name)` -- decrement refcount, auto-stop when zero
- `Stream(name)` -- channel-based streaming with 1-second timeout
- **VideoQueue exception:** not stopped when `video_enabled=true`
- **Graceful shutdown:** all services stopped in reverse order on exit

### Core Services

| Service | Description | Complexity |
|---------|-------------|------------|
| **MqttQueue** | Heart of the app. Handles 39 MQTT message types (`ct` values). State machine for print status (`ct=1000`): 0=idle, 1=printing, 2=paused, 8=aborted. Progress normalization: MQTT sends 0-10000, API exposes 0-100. | Very High |
| **PPPPService** | LAN connection via PPPP protocol. Xzyh handler pattern. Restarts on `ConnectionReset`. | High |
| **VideoQueue** | H.264 camera stream. Stall detection at 15 seconds. Profiles: sd (848x480), hd (1280x720), fhd (1920x1080, snapshot-only). | Medium |
| **FileTransferService** | GCode patching + upload via PPPP channels. Layer extraction for progress tracking. | Medium |
| **PrintHistory** | SQLite-backed print log. Auto-retention: 90 days, max 500 entries. | Low |
| **TimelapseService** | Periodic camera snapshots assembled into MP4 via ffmpeg. Resume window: 60 min. Orphan recovery: up to 24 hours. | High |
| **HomeAssistantService** | Publishes HA MQTT Discovery payloads (11 sensors, 2 binary, 1 switch, 1 camera) to an external broker. 60-second heartbeat. Bidirectional light switch. | Medium |
| **FilamentStore** | SQLite-backed filament profile storage. 35+ columns per profile. | Low |

---

## Web Layer

HTTP server using **chi/v5**. Default listen address: `127.0.0.1:4470`.

### Middleware Order (outer to inner)

1. **Recovery** -- panic handler
2. **Request ID** -- unique ID per request
3. **Access Logging** -- structured request/response logging
4. **Security Headers** -- CSP, X-Frame-Options, etc.
5. **Rate Limiting** -- IP-based
6. **Body Size Limit** -- `UPLOAD_MAX_MB`
7. **Require Printer** -- reject requests when no printer is configured
8. **Block Unsupported Device** -- reject unsupported printer models
9. **Auth** -- API key check

### Auth Rules

These rules must match the Python implementation exactly:

| Method | Rule |
|--------|------|
| `GET` | Unauthenticated by default |
| `POST` / `DELETE` | Always require API key |
| Protected GET paths | `/api/ankerctl/server/reload`, `/api/debug/*`, `/api/settings/mqtt`, `/api/notifications/settings` |
| Setup exemptions (no printer) | `/api/ankerctl/config/upload`, `/api/ankerctl/config/login` |

### WebSocket Endpoints

| Path | Direction | Purpose |
|------|-----------|---------|
| `/ws/mqtt` | Server -> Client | MQTT event stream |
| `/ws/video` | Server -> Client | Binary H.264 video stream |
| `/ws/pppp-state` | Server -> Client | PPPP connection status polling |
| `/ws/upload` | Server -> Client | File transfer progress |
| `/ws/ctrl` | Bidirectional | Light control, video profile, video enable (has inline auth) |

### Frontend

The frontend is unchanged from the Python original. HTML templates are converted from Jinja2 to `html/template` syntax. All static files are embedded into the binary via `//go:embed static/*`.

---

## Python Source Mapping

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
| `internal/web` | `web/__init__.py` (~1700 LOC, 40+ routes) |
| `internal/notifications` | `libflagship/notifications/`, `web/notifications.py` |
| `internal/gcode` | `cli/util.py` (gcode functions) |

---

## Dependencies

| Package | Purpose |
|---------|---------|
| [chi/v5](https://github.com/go-chi/chi) | HTTP router (stdlib-compatible) |
| [gorilla/websocket](https://github.com/gorilla/websocket) | WebSocket |
| [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) | SQLite (CGO-free, pure Go, multi-arch safe) |
| [paho.mqtt.golang](https://github.com/eclipse/paho.mqtt.golang) | MQTT client (Eclipse) |
| [cobra](https://github.com/spf13/cobra) | CLI framework |
| [google/uuid](https://github.com/google/uuid) | UUID generation |
| `log/slog` (stdlib) | Structured logging |
| `crypto/aes`, `crypto/ecdh` (stdlib) | AES-256-CBC, secp256r1 |
| `html/template` (stdlib) | Jinja2 replacement |
