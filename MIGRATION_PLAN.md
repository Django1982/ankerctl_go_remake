# ankerctl Go Migration Plan

**Status**: Fortschritt bis inkl. Phase 12 dokumentiert (siehe `docs/agents/reports/`)
**Erstellt**: 2026-03-03
**Ziel**: 1:1 Feature-Parity mit dem Python-Original

---

## Inhaltsverzeichnis

1. [Architektur-Uebersicht](#architektur-uebersicht)
2. [Python-zu-Go Paket-Mapping](#python-zu-go-paket-mapping)
3. [Bibliotheks-Empfehlungen](#bibliotheks-empfehlungen)
4. [Aktuelle Status-Matrix](#aktuelle-status-matrix)
5. [Migrationsphasen](#migrationsphasen)
6. [Sicherheits-Checkliste](#sicherheits-checkliste)
7. [Kritische Protokoll-Konstanten](#kritische-protokoll-konstanten)

---

## Architektur-Uebersicht

### Go-Projektstruktur

```
ankerctl_go_remake/
  cmd/
    ankerctl/               # CLI entry point (main.go)
  internal/
    config/                 # Config parser, data models, ConfigManager
    crypto/                 # AES-256-CBC, ECDH, XOR checksums
    mqtt/
      protocol/             # MqttMsg, MqttPktType, MqttMsgType (from libflagship/mqtt.py)
      client/               # MQTT client wrapper (from libflagship/mqttapi.py)
    pppp/
      protocol/             # PPPP packet types, Message parsing (from libflagship/pppp.py)
      client/               # PPPP API, Channels, Wire (from libflagship/ppppapi.py)
      crypto/               # PPPP crypto_curse/decurse (from libflagship/megajank.py)
    httpapi/                # Anker Cloud HTTP API (from libflagship/httpapi.py)
    service/                # Service framework + all services
      manager.go            # ServiceManager with ref-counting
      service.go            # Service interface + lifecycle
      mqttqueue.go          # MqttQueue (from web/service/mqtt.py)
      pppp.go               # PPPPService (from web/service/pppp.py)
      video.go              # VideoQueue (from web/service/video.py)
      filetransfer.go       # FileTransferService (from web/service/filetransfer.py)
      history.go            # PrintHistory SQLite (from web/service/history.py)
      timelapse.go          # TimelapseService (from web/service/timelapse.py)
      homeassistant.go      # HA MQTT Discovery (from web/service/homeassistant.py)
      filament.go           # FilamentStore SQLite (from web/service/filament.py)
    web/
      server.go             # HTTP server setup, middleware stack
      routes.go             # All REST endpoints
      handler/              # Grouped HTTP handler functions
        general.go          # /, /api/health, /api/version, /video
        config.go           # /api/ankerctl/config/*, /api/ankerctl/server/reload
        printer.go          # /api/printer/*, /api/snapshot
        notification.go     # /api/notifications/*
        settings.go         # /api/settings/*
        history.go          # /api/history
        timelapse.go        # /api/timelapses, /api/timelapse/*
        filament.go         # /api/filaments, /api/filaments/*
        printers.go         # /api/printers, /api/printers/active
        slicer.go           # /api/files/local (OctoPrint compat)
        debug.go            # /api/debug/* (ANKERCTL_DEV_MODE only)
        bedlevel.go         # /api/printer/bed-leveling
        snapshot.go         # /api/snapshot
        video.go            # /video stream
      ws/                   # WebSocket handlers
        mqtt.go             # /ws/mqtt
        video.go            # /ws/video
        ppppstate.go        # /ws/pppp-state
        upload.go           # /ws/upload
        ctrl.go             # /ws/ctrl (bidirectional, inline auth)
      middleware/            # HTTP middleware
        auth.go             # API key authentication
        security.go         # Security headers (CSP, X-Frame-Options, etc.)
        ratelimit.go        # IP-based rate limiting
        logging.go          # Access logging
      templates.go          # Go html/template rendering
    notifications/          # Apprise notification system
      apprise.go            # Apprise HTTP client
      notifier.go           # AppriseNotifier lifecycle
      events.go             # Event constants
    gcode/                  # GCode parsing utilities
      time.go               # patch_gcode_time
      layers.go             # extract_layer_count
    model/                  # Shared data models
      printer.go            # Printer struct
      account.go            # Account struct
      config.go             # Config struct with JSON serialization
      defaults.go           # Default configurations
    util/                   # Shared utilities
      encoding.go           # enhex/unhex, b64e/b64d equivalents
      format.go             # format_duration, format_bytes, pretty_json
      ratelimit.go          # Upload rate limiter
  static/                   # 1:1 copy of frontend (HTML/JS/CSS/vendor)
  go.mod
  go.sum
  Dockerfile
  docker-compose.yaml
  MIGRATION_PLAN.md
  README.md
  LICENSE
```

---

## Python-zu-Go Paket-Mapping

### Protocol Layer (libflagship/)

| Python Source | Go Package | Description |
|---|---|---|
| `libflagship/mqtt.py` | `internal/mqtt/protocol` | MqttMsg, MqttPktType, MqttMsgType (auto-generated) |
| `libflagship/mqttapi.py` | `internal/mqtt/client` | MQTT client, subscribe/publish, fetch/command |
| `libflagship/pppp.py` | `internal/pppp/protocol` | PPPP packet types, Message.parse, Xzyh, Aabb |
| `libflagship/ppppapi.py` | `internal/pppp/client` | AnkerPPPPApi, Channel, Wire, PPPPState |
| `libflagship/megajank.py` (AES/ECDH) | `internal/crypto` | AES-CBC, ECDH, XOR checksums |
| `libflagship/megajank.py` (PPPP) | `internal/pppp/crypto` | crypto_curse/decurse, initstring decoder |
| `libflagship/httpapi.py` | `internal/httpapi` | Anker Cloud REST API |
| `libflagship/seccode.py` | `internal/httpapi` | Check code / sec code calculation |
| `libflagship/amtypes.py` | `internal/mqtt/protocol` | Struct definitions |
| `libflagship/cyclic.py` | `internal/pppp/protocol` | CyclicU16 counter |
| `libflagship/pktdump.py` | `internal/pppp/client` | Packet debug dump writer |
| `libflagship/notifications/` | `internal/notifications` | Apprise HTTP client + events |
| `libflagship/logincache.py` | `internal/config` | Region detection from login.json |
| `libflagship/util.py` | `internal/util` | enhex/unhex, b64e/b64d |

### Service Layer (web/service/)

| Python Source | Go Package | LOC | Complexity |
|---|---|---|---|
| `web/lib/service.py` | `internal/service` | 375 | HIGH - Lifecycle state machine |
| `web/service/mqtt.py` | `internal/service` | 724 | VERY HIGH - Core of the app |
| `web/service/pppp.py` | `internal/service` | 194 | HIGH - UDP asymmetry |
| `web/service/video.py` | `internal/service` | 212 | MEDIUM |
| `web/service/filetransfer.py` | `internal/service` | 137 | MEDIUM |
| `web/service/history.py` | `internal/service` | 225 | LOW |
| `web/service/timelapse.py` | `internal/service` | 665 | HIGH - ffmpeg, resume window |
| `web/service/homeassistant.py` | `internal/service` | 502 | MEDIUM |
| `web/service/filament.py` | `internal/service` | 411 | LOW |

### API Layer (web/ + cli/)

| Python Source | Go Package | Description |
|---|---|---|
| `web/__init__.py` | `internal/web` | ~1700 LOC: 40+ routes, 5 WS, middleware |
| `web/notifications.py` | `internal/notifications` | AppriseNotifier, snapshot capture |
| `cli/config.py` | `internal/config` | ConfigManager, config import |
| `cli/model.py` | `internal/model` | Data models (Config, Account, Printer) |
| `cli/util.py` | `internal/util` + `internal/gcode` | GCode patching, rate limiting, helpers |
| `cli/mqtt.py` | `cmd/ankerctl` + `internal/mqtt/client` | CLI commands |
| `cli/pppp.py` | `cmd/ankerctl` + `internal/pppp/client` | CLI commands |

### Frontend (static/)

| Source | Action |
|---|---|
| `static/*.html` | Copy 1:1, convert Jinja2 to Go `html/template` |
| `static/*.js` | Copy 1:1 (no framework change) |
| `static/*.css` | Copy 1:1 |
| `static/vendor/` | Copy 1:1 |
| `static/libflagship.js` | Copy 1:1 |

---

## Library Recommendations

### Primary Libraries

| Purpose | Go Package | Rationale |
|---|---|---|
| **HTTP Router** | `github.com/go-chi/chi/v5` | Lightweight, stdlib-compatible |
| **WebSocket** | `github.com/gorilla/websocket` | De-facto standard |
| **SQLite** | `modernc.org/sqlite` | CGO-free, pure Go |
| **MQTT Client** | `github.com/eclipse/paho.mqtt.golang` | Official Eclipse library |
| **CLI** | `github.com/spf13/cobra` | Standard Go CLI framework |
| **Logging** | `log/slog` (stdlib) | Structured logging, Go 1.21+ |
| **Crypto AES** | `crypto/aes` + `crypto/cipher` (stdlib) | AES-256-CBC native |
| **Crypto ECDH** | `crypto/ecdh` + `crypto/elliptic` (stdlib) | secp256r1 native |
| **Templates** | `html/template` (stdlib) | Jinja2 replacement |
| **HTTP Client** | `net/http` (stdlib) | For Anker Cloud API + Apprise |
| **UUID** | `github.com/google/uuid` | File upload IDs |

---

## Aktuelle Status-Matrix

**Stand**: 2026-03-05  
**Quelle**: Code-Stand + `docs/agents/reports/*`

| Phase | Status | Kurzkommentar |
|---|---|---|
| 1. Project Scaffold | done | Basisstruktur steht |
| 2. Config + Models | done | Implementiert, Tests vorhanden |
| 3. Crypto Layer | done | Implementiert, Tests vorhanden |
| 4. Middleware + HTTP Server | done | Implementiert, Tests + race/vet gruen |
| 5. SQLite DB Layer | partial | Teile in Services/Handlern vorhanden, Feinschliff offen |
| 6. MQTT Protocol + Client | done | Report `phase6-codex`: done |
| 7. PPPP Protocol + Client | partial | Report `phase7-codex`: partial |
| 8. Service Framework | done | Report `phase8-codex`: done |
| 9. Web Services | done | Reports `phase9a/9b-codex`: done |
| 10. HTTP API Handlers | partial | Report `phase10-codex`: partial |
| 11. WebSocket Handlers | done | Report `phase11-codex`: done, Review vorhanden |
| 12. Notifications + GCode Utils | done | Report `phase12-codex`: DONE |
| 13. Anker Cloud HTTP API | open | Geplant, kein Abschlussreport |
| 14. Frontend + Templates | open | Geplant, kein Abschlussreport |
| 15. CLI Commands | open | Geplant, kein Abschlussreport |
| 16. Docker + CI | open | Geplant, kein Abschlussreport |

Aktive Restpunkte werden zentral in [`docs/agents/OPEN_ITEMS.md`](docs/agents/OPEN_ITEMS.md) gepflegt.

---

## Migration Phases

### Phase 1: Project Scaffold (DONE)

Go module initialized, directory structure created, placeholder files in place.

- `go.mod` with `module github.com/django1982/ankerctl`
- All directories from architecture overview
- `doc.go` per package with description
- `cmd/ankerctl/main.go` with empty entry point

---

### Phase 2: Config Parser + Data Models (Day 2-3)

**Goal**: Read/write config files with strict Go types

**Python sources**: `cli/model.py`, `cli/config.py`, `libflagship/logincache.py`

**Go Structs**:
```go
type Config struct {
    Account            *Account            `json:"account"`
    Printers           []Printer           `json:"printers"`
    UploadRateMbps     int                 `json:"upload_rate_mbps"`
    Notifications      map[string]any      `json:"notifications"`
    Timelapse          map[string]any      `json:"timelapse"`
    HomeAssistant      map[string]any      `json:"home_assistant"`
    ActivePrinterIndex int                 `json:"active_printer_index"`
}

type Printer struct {
    ID         string  `json:"id"`
    SN         string  `json:"sn"`
    Name       string  `json:"name"`
    Model      string  `json:"model"`
    CreateTime float64 `json:"create_time"`  // Unix timestamp
    UpdateTime float64 `json:"update_time"`  // Unix timestamp
    WifiMAC    string  `json:"wifi_mac"`
    IPAddr     string  `json:"ip_addr"`
    MQTTKey    string  `json:"mqtt_key"`     // hex-encoded
    APIHosts   string  `json:"api_hosts"`    // obfuscated string
    P2PHosts   string  `json:"p2p_hosts"`    // obfuscated string
    P2PDUID    string  `json:"p2p_duid"`
    P2PKey     string  `json:"p2p_key"`
}

type Account struct {
    AuthToken string `json:"auth_token"`
    Region    string `json:"region"`
    UserID    string `json:"user_id"`
    Email     string `json:"email"`
    Country   string `json:"country"`
}
```

**Note**: The `__type__` field in JSON is used by Python for polymorphic deserialization.
Go must handle this during JSON unmarshaling (custom UnmarshalJSON).

**Security**:
- Config directory chmod 0700
- Token redaction in logs
- Strict JSON deserialization

---

### Phase 3: Crypto Layer (Day 3-4)

**Goal**: Port all crypto functions from `megajank.py`

**Sub-modules**:
1. `internal/crypto/aes.go` - AES-256-CBC with IV `3DPrintAnkerMake`
2. `internal/crypto/mqtt.go` - mqtt_checksum_add/remove (XOR)
3. `internal/crypto/ecdh.go` - ECDH login password encryption
4. `internal/pppp/crypto/` - crypto_curse/decurse, simple_encrypt/decrypt, initstring decoder

**Critical**: PKCS7 padding, fixed IV (16 bytes), Anker EC public key (secp256r1),
PPPP shuffle tables must be bit-exact.

---

### Phase 4: Middleware Stack + HTTP Server (Day 4-6)

**Goal**: Empty HTTP server with complete security stack

**Middleware order** (outside to inside):
1. Recovery (panic handler)
2. Request ID
3. Access logging
4. Security headers
5. Rate limiting (IP-based)
6. Body size limit
7. Auth middleware (API key check)

**Auth rules** (exactly like Python):
- GET: unauthenticated by default
- POST/DELETE: always require auth
- Protected GET paths: `/api/ankerctl/server/reload`, `/api/debug/state`,
  `/api/debug/logs`, `/api/debug/services`, `/api/settings/mqtt`,
  `/api/notifications/settings`
- All `/api/debug/*` paths: auth required (prefix match)
- Setup paths exempt when no printer configured:
  `/api/ankerctl/config/upload`, `/api/ankerctl/config/login`

---

### Phase 5: SQLite DB Layer (Day 6-7)

**Goal**: Print history and filament profiles with SQLite

**History schema**: id, filename, status, started_at, finished_at, duration_sec,
progress, failure_reason, task_id

**Filament schema**: 35+ columns (see `web/service/filament.py`)

**Critical behaviors**:
- Auto-migration via ALTER TABLE
- Placeholder filter: `"unknown"`, `"unknown.gcode"`, `""` -> skip
- Retention: 90 days, max 500 entries
- Thread safety via `sync.Mutex`
- Parameterized queries everywhere
- XSS sanitization on text fields

---

### Phase 6: MQTT Protocol + Client (Day 7-10)

**Goal**: MQTT connection to Anker broker, encrypted communication

**Topics**:
- Subscribe: `/phone/maker/{SN}/notice`, `/phone/maker/{SN}/command/reply`,
  `/phone/maker/{SN}/query/reply`
- Publish: `/device/maker/{SN}/command`, `/device/maker/{SN}/query`

**Packet structure**: Header (63 bytes) + AES-256-CBC encrypted body + XOR checksum

**All 39 MqttMsgType values must be ported.**

---

### Phase 7: PPPP Protocol + Client (Day 10-14)

**Goal**: UDP-based P2P protocol for LAN communication

**Critical aspects**:
- UDP asymmetry: one socket, 8 logical channels
- DRW pipelining: in-flight window (max 64), retransmission (0.5s timeout)
- CyclicU16: 16-bit wraparound counter
- Xzyh frames: 16-byte header + payload on channel 0/1
- Aabb frames: file transfer on channel 1, CRC-protected
- LAN discovery: broadcast on port 32108

**Go concurrency mapping**:
- `Channel.poll()` -> goroutine with ticker
- `Channel.write()` blocking -> channel + WaitGroup
- `Wire` (Pipe) -> `chan []byte`
- `AnkerPPPPBaseApi.run()` -> goroutine with select loop

---

### Phase 8: Service Framework (Day 14-16)

**Goal**: Service lifecycle and ServiceManager in Go

**Service interface**:
```go
type Service interface {
    WorkerInit()
    WorkerStart() error
    WorkerRun(timeout time.Duration) error
    WorkerStop()
    Name() string
    State() RunState
    Start()
    Stop()
    Restart()
    Shutdown()
    Notify(data any)
    Tap(handler func(any)) func() // returns cleanup func
}
```

**ServiceManager**:
- Ref-counted get/put with auto-start/stop
- Borrow pattern (context manager equivalent)
- Stream pattern (channel-based, 1s timeout)
- VideoQueue exception: don't stop when video_enabled=true
- Graceful shutdown with atexit equivalent

**State machine**: Starting -> Running -> Stopping -> Stopped
**Holdoff**: delayed restarts (1s cooldown)
**ServiceRestartSignal**: sentinel error for clean restart

---

### Phase 9: Web Services (Day 16-22)

**Goal**: All 8 service implementations

**MqttQueue** (VERY HIGH complexity):
- ct=1000 state machine (0=idle, 1=printing, 2=paused, 8=aborted)
- Progress normalization (0-10000 -> 0-100)
- Print control (0=restart, 2=pause, 3=resume, 4=stop)
- ct=1044 filename capture with deferred history start
- Sub-services: PrintHistory, TimelapseService, HomeAssistantService
- Forward to HA, handle notifications, emit progress events

**PPPPService**: LAN connection, xzyh handler pattern, ConnectionReset -> restart

**VideoQueue**: Profiles (sd/hd/fhd), stall detection (15s), light control, generation counter

**FileTransferService**: GCode patching, layer extraction, PPPP file transfer, progress callback

**TimelapseService**: Periodic snapshots, resume window (60min), ffmpeg assembly,
persistent frames with .meta sidecar, orphan recovery, light control modes

**HomeAssistantService**: External MQTT, discovery payloads (11 sensors, 2 binary, 1 switch, 1 camera),
LWT, availability heartbeat (60s), bidirectional light switch

**PrintHistory + FilamentStore**: Already covered in Phase 5, integrated here.

---

### Phase 10: HTTP API Handlers (Day 22-26)

**Goal**: All 40+ REST endpoints

**Endpoint groups**:
1. General (4): `/`, `/api/health`, `/api/version`, `/video`
2. Configuration (4): config upload, login, reload, upload-rate
3. Printer Control (5): gcode, control, autolevel, bed-leveling, snapshot
4. Notifications (3): settings GET/POST, test
5. Settings (4): timelapse GET/POST, mqtt GET/POST
6. History (2): list GET, clear DELETE
7. Timelapse (3): list, download, delete
8. Filament (6): list, create, update, delete, apply, duplicate
9. Printer Selector (2): list, switch active
10. Slicer (1): OctoPrint-compatible file upload
11. Debug (8): state, config, simulate, services, restart, logs (ANKERCTL_DEV_MODE only)
12. Bed Leveling (2): live + last saved

---

### Phase 11: WebSocket Handlers (Day 26-28)

**Goal**: All 5 WebSocket streams

| Path | Direction | Source |
|---|---|---|
| `/ws/mqtt` | Server->Client | Stream from mqttqueue |
| `/ws/video` | Server->Client | Stream from videoqueue (binary H.264) |
| `/ws/pppp-state` | Server->Client | PPPP connection status polling |
| `/ws/upload` | Server->Client | Stream from filetransfer |
| `/ws/ctrl` | Bidirectional | Light/video profile/video enable (inline auth!) |

---

### Phase 12: Notifications + GCode Utils (Day 28-31)

**Apprise**: HTTP client to API server, event rendering with templates, snapshot via ffmpeg

**GCode**: `patch_gcode_time` (insert ;TIME: before G28), `extract_layer_count`
(OrcaSlicer ;LAYER_COUNT, PrusaSlicer ;LAYER_CHANGE counting)

---

### Phase 13: Anker Cloud HTTP API (Day 31-33)

**Goal**: Port all Anker API client classes

- AnkerHTTPPassportApiV1: profile
- AnkerHTTPPassportApiV2: login (ECDH)
- AnkerHTTPAppApiV1: query_fdm_list, equipment_get_dsk_keys
- AnkerHTTPHubApiV1/V2: query_device_info, OTA, P2P connect
- Region detection (closest host by TCP connect time)
- Gtoken header: MD5 of user_id

---

### Phase 14: Frontend + Templates (Day 33-35)

**Goal**: Static file serving, Jinja2 -> Go templates

- Copy `static/` directory 1:1
- Convert Jinja2 syntax: `{% block %}` -> `{{template}}`, `{{ var }}` -> `{{.Var}}`
- Static file server with cache headers
- Embed via `//go:embed static/*`

---

### Phase 15: CLI Commands (Day 35-37)

**Goal**: All CLI commands with cobra (secondary priority)

```
ankerctl config import/login/show/set-password/remove-password/decode
ankerctl mqtt monitor/gcode/gcode-dump/rename-printer/send
ankerctl pppp lan-search/print-file/capture-video
ankerctl http calc-check-code/calc-sec-code
ankerctl webserver run
```

---

### Phase 16: Docker + CI (Day 37-39)

**Goal**: Multi-arch Docker build, health check

```dockerfile
FROM golang:1.22-alpine AS builder
# ... build ...
FROM alpine:3.19
RUN apk add --no-cache ffmpeg ca-certificates
COPY --from=builder /app/ankerctl /usr/local/bin/
```

Benefits: ~50MB image (vs ~300MB), faster start, single binary.

---

## Security Checklist

### Per Phase

| Phase | Security Items |
|---|---|
| 2 (Config) | Config dir chmod 0700, token redaction, strict types |
| 3 (Crypto) | AES IV exact, PKCS7 padding, EC key correct, no timing leaks |
| 4 (HTTP) | Security headers, auth middleware, rate limit, body limit |
| 5 (SQLite) | Prepared statements, input validation, XSS sanitization |
| 6 (MQTT) | TLS connection, AES encryption, key from config |
| 7 (PPPP) | LAN only, timeout handling, no open ports |
| 8 (Services) | Ref counting, graceful shutdown, no goroutine leaks |
| 10 (Routes) | Auth on POST/DELETE, protected GET paths, 409 during print |
| 11 (WS) | Auth on /ws/ctrl, timeout on streams, no memory leak |
| 14 (Frontend) | CSP header, correct Content-Type on static files |
| 16 (Docker) | Non-root user, health check |

### Global

- [ ] No secrets in logs (auth_token, mqtt_key, api_key)
- [ ] No panics in production paths (always return error)
- [ ] Graceful shutdown of all goroutines (context.Context)
- [ ] SQL injection protection (parameterized queries)
- [ ] XSS protection (HTML tag stripping in filament fields)
- [ ] Path traversal protection (timelapse download, log viewer)
- [ ] Upload size limit (UPLOAD_MAX_MB)
- [ ] API key validation (min 16 chars, pattern [a-zA-Z0-9_-])

---

## Critical Protocol Constants

These values MUST be implemented exactly:

```go
// MQTT
const MQTTAesIV = "3DPrintAnkerMake"  // 16 bytes, fixed
const TopicNotice   = "/phone/maker/%s/notice"
const TopicReply    = "/phone/maker/%s/command/reply"
const TopicQueryRep = "/phone/maker/%s/query/reply"
const TopicCommand  = "/device/maker/%s/command"
const TopicQuery    = "/device/maker/%s/query"
const MQTTPort = 8789

// Print Control (ct=1008)
const PrintControlRestart = 0
const PrintControlPause   = 2
const PrintControlResume  = 3
const PrintControlStop    = 4

// State Machine (ct=1000)
const StateIdle     = 0
const StatePrinting = 1
const StatePaused   = 2
const StateAborted  = 8

// Progress: MQTT 0-10000, API 0-100 (divide by 100)

// Web Server
const DefaultHost = "127.0.0.1"
const DefaultPort = 4470

// Timelapse
const ResumeWindowSec = 3600  // 60 minutes
const MaxOrphanAgeSec = 86400 // 24 hours
// FPS: max(1, min(30, ceil(frame_count/30)))

// Video
const StallTimeoutSec = 15.0
// Profiles: sd (848x480), hd (1280x720), fhd (1920x1080 snapshot-only)

// History
const DefaultRetentionDays = 90
const DefaultMaxEntries    = 500
// Placeholders: "unknown", "unknown.gcode", ""

// PPPP
const PPPPLanPort = 32108
const PPPPWanPort = 32100
const PPPPSeed = "EUPRAKM"
const PPPPSimpleSeed = "SSD@cs2-network."

// Anker EC Public Key (secp256r1)
const AnkerECPubKeyX = "C5C00C4F8D1197CC7C3167C52BF7ACB054D722F0EF08DCD7E0883236E0D72A38"
const AnkerECPubKeyY = "68D9750CB47FA4619248F3D83F0F662671DADC6E2D31C2F41DB0161651C7C076"

// MqttMsg Header Constants
const MqttMsgM3 = 5
const MqttMsgM4 = 1
const MqttMsgM5 = 2
const MqttMsgM6 = 5
const MqttMsgM7 = 0x46 // 'F'
```

---

## Environment Variables (Complete List)

All env vars from the Python project must be supported identically:

**Server**: PRINTER_INDEX, FLASK_HOST, FLASK_PORT, FLASK_SECRET_KEY, UPLOAD_MAX_MB
**Security**: ANKERCTL_API_KEY
**Features**: ANKERCTL_DEV_MODE, ANKERCTL_LOG_DIR, UPLOAD_RATE_MBPS
**Apprise**: APPRISE_ENABLED, APPRISE_SERVER_URL, APPRISE_KEY, APPRISE_TAG,
  APPRISE_EVENT_PRINT_STARTED, APPRISE_EVENT_PRINT_FINISHED, APPRISE_EVENT_PRINT_FAILED,
  APPRISE_EVENT_GCODE_UPLOADED, APPRISE_EVENT_PRINT_PROGRESS,
  APPRISE_PROGRESS_INTERVAL, APPRISE_PROGRESS_INCLUDE_IMAGE,
  APPRISE_SNAPSHOT_QUALITY, APPRISE_SNAPSHOT_FALLBACK, APPRISE_SNAPSHOT_LIGHT,
  APPRISE_PROGRESS_MAX
**History**: PRINT_HISTORY_RETENTION_DAYS, PRINT_HISTORY_MAX_ENTRIES
**Timelapse**: TIMELAPSE_ENABLED, TIMELAPSE_INTERVAL_SEC, TIMELAPSE_MAX_VIDEOS,
  TIMELAPSE_SAVE_PERSISTENT, TIMELAPSE_CAPTURES_DIR, TIMELAPSE_LIGHT
**Home Assistant**: HA_MQTT_ENABLED, HA_MQTT_HOST, HA_MQTT_PORT, HA_MQTT_USER,
  HA_MQTT_PASSWORD, HA_MQTT_DISCOVERY_PREFIX, HA_MQTT_TOPIC_PREFIX

---

## Estimated Timeline

| Phase | Days | Dependencies |
|---|---|---|
| 1. Project Scaffold | DONE | - |
| 2. Config Parser | 2 | Phase 1 |
| 3. Crypto Layer | 2 | Phase 1 |
| 4. HTTP + Middleware | 3 | Phase 2 |
| 5. SQLite DB Layer | 2 | Phase 2 |
| 6. MQTT Protocol | 4 | Phase 3 |
| 7. PPPP Protocol | 5 | Phase 3 |
| 8. Service Framework | 3 | Phase 4 |
| 9. Web Services | 6 | Phase 6, 7, 8 |
| 10. HTTP Handlers | 5 | Phase 5, 9 |
| 11. WebSocket | 3 | Phase 9 |
| 12. Notifications + GCode | 3 | Phase 9 |
| 13. Anker Cloud API | 3 | Phase 3 |
| 14. Frontend | 3 | Phase 10, 11 |
| 15. CLI | 3 | Phase 13 |
| 16. Docker + CI | 2 | Phase 14 |
| **Total** | **~38 days** | |

Parallelizable: Phase 2+3, Phase 5+6+7, Phase 10+11+12, Phase 14+15
Realistic with parallelization: **~25 working days**

---

## Risk Register

| Risk | Severity | Mitigation |
|---|---|---|
| PPPP UDP asymmetry complex to port | High | Start with protocol parsing, test incrementally |
| Auto-generated pppp.py/mqtt.py (transwarp) | Medium | Manual port; these are stable |
| ECDH login password encryption | Medium | Go stdlib crypto/ecdh (1.20+); verify curve params |
| ffmpeg subprocess dependency | Low | Same subprocess approach in Go (exec.Command) |
| WebSocket streaming semantics | Medium | gorilla/websocket; notify pattern with channels |
| Service lifecycle (ref-counting) | High | Design Go ServiceManager carefully; use sync patterns |
| Python __type__ JSON polymorphism | Medium | Custom JSON unmarshal in Go |
