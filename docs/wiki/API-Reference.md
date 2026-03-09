# API Reference

ankerctl exposes a REST API and WebSocket endpoints. The default server address is `127.0.0.1:4470`.

## Authentication

- **No API key set:** All endpoints are public (default, backwards compatible)
- **API key set:** POST/DELETE always require auth; some GET paths are also protected
- **Auth methods:**
  - Header: `X-Api-Key: <key>`
  - Query parameter: `?apikey=<key>` (sets a session cookie)
  - Session cookie (set automatically after query parameter auth)

### Protected GET Paths

These GET endpoints require authentication when an API key is configured:

- `/api/ankerctl/server/reload`
- `/api/debug/*` (all debug endpoints)
- `/api/settings/mqtt`
- `/api/notifications/settings`
- `/api/printers`
- `/api/history`

### Setup-Exempt Paths

These POST endpoints are accessible without auth when no printer is configured (initial setup):

- `/api/ankerctl/config/upload`
- `/api/ankerctl/config/login`

## REST Endpoints

### General

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/` | No | Web UI dashboard |
| GET | `/video` | No | Video stream page |
| GET | `/api/health` | No | Health check (returns `{"status": "ok"}`) |
| GET | `/api/version` | No | Version info |
| GET | `/api/snapshot` | No | Camera snapshot (JPEG) |

### Configuration

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/ankerctl/config/upload` | Yes* | Upload login.json config file |
| POST | `/api/ankerctl/config/login` | Yes* | Login with email/password |
| POST | `/api/ankerctl/config/logout` | Yes | Clear stored credentials |
| GET | `/api/ankerctl/server/reload` | Yes | Reload config and restart services |
| POST | `/api/ankerctl/config/upload-rate` | Yes | Set upload speed (5/10/25/50/100 Mbit/s) |

*Setup-exempt when no printer is configured.

### Printer Control

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/printer/gcode` | Yes | Send G-code command |
| POST | `/api/printer/control` | Yes | Print control (pause/resume/stop) |
| POST | `/api/printer/autolevel` | Yes | Start auto bed leveling (G29) |
| GET | `/api/printer/bed-leveling` | No | Read bed level grid (sends M420 V) |
| GET | `/api/printer/bed-leveling/last` | No | Last saved bed level grid |

**Print control values** (POST body `{"value": N}`):
- `0` -- Restart print
- `2` -- Pause
- `3` -- Resume
- `4` -- Stop

### Printer Selector

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/printers` | Yes | List configured printers |
| POST | `/api/printers/active` | Yes | Switch active printer |

### File Upload (OctoPrint Compatible)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/files/local` | Yes | Upload G-code file (multipart/form-data) |

This endpoint is compatible with PrusaSlicer, OrcaSlicer, and other OctoPrint-compatible slicers. The uploaded file is patched (estimated time injection, layer count extraction) and sent to the printer via PPPP.

### Notifications

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/notifications/settings` | Yes | Get notification config |
| POST | `/api/notifications/settings` | Yes | Update notification config |
| POST | `/api/notifications/test` | Yes | Send test notification |

### Settings

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/settings/timelapse` | No | Get timelapse config |
| POST | `/api/settings/timelapse` | Yes | Update timelapse config |
| GET | `/api/settings/mqtt` | Yes | Get Home Assistant MQTT config |
| POST | `/api/settings/mqtt` | Yes | Update Home Assistant MQTT config |

### History

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/history` | Yes | List print history (`?limit=N&offset=N`) |
| DELETE | `/api/history` | Yes | Clear all history entries |

### Filaments

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/filaments` | No | List all filament profiles |
| POST | `/api/filaments` | Yes | Create new filament profile |
| PUT | `/api/filaments/{id}` | Yes | Update filament profile |
| DELETE | `/api/filaments/{id}` | Yes | Delete filament profile |
| POST | `/api/filaments/{id}/apply` | Yes | Apply filament settings to printer |
| POST | `/api/filaments/{id}/duplicate` | Yes | Duplicate a filament profile |

### Filament Service

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/filaments/service/swap` | No | Get current swap state |
| POST | `/api/filaments/service/preheat` | Yes | Preheat nozzle/bed |
| POST | `/api/filaments/service/move` | Yes | Move extruder (extrude/retract) |
| POST | `/api/filaments/service/swap/start` | Yes | Start filament swap wizard |
| POST | `/api/filaments/service/swap/confirm` | Yes | Confirm swap step |
| POST | `/api/filaments/service/swap/cancel` | Yes | Cancel swap wizard |

### Timelapses

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/timelapses` | No | List available timelapse videos |
| GET | `/api/timelapse/{filename}` | No | Download timelapse video |
| DELETE | `/api/timelapse/{filename}` | Yes | Delete timelapse video |

### Debug (ANKERCTL_DEV_MODE=true only)

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/debug/state` | Yes | Dump current print state as JSON |
| POST | `/api/debug/config` | Yes | Toggle verbose MQTT logging |
| POST | `/api/debug/simulate` | Yes | Fire synthetic events |
| GET | `/api/debug/logs` | Yes | List log files |
| GET | `/api/debug/logs/{filename}` | Yes | Read log file contents |
| GET | `/api/debug/services` | Yes | Service health and ref counts |
| GET | `/api/debug/video/stats` | Yes | Video stream statistics |
| POST | `/api/debug/services/{name}/restart` | Yes | Restart a service |
| POST | `/api/debug/services/{name}/test` | Yes | Test a service |
| POST | `/api/debug/pppp/discover` | Yes | Trigger printer IP discovery |
| POST | `/api/debug/pppp/reconnect` | Yes | Force PPPP reconnect |
| GET | `/api/debug/bed-leveling` | Yes | Bed level grid (debug alias) |

## WebSocket Endpoints

All WebSocket endpoints use the standard upgrade handshake on the same HTTP port.

### /ws/mqtt

**Direction:** Server to Client

Streams MQTT events from the printer as JSON messages. Each message contains the command type and decoded payload.

### /ws/video

**Direction:** Server to Client

Streams binary H.264 NAL units from the printer camera. Connect to receive a live video feed.

### /ws/pppp-state

**Direction:** Server to Client

Polls PPPP connection state and sends updates. Includes connection status, printer IP, and signal quality.

### /ws/upload

**Direction:** Server to Client

Streams file transfer progress as JSON messages during G-code uploads. Reports bytes sent, total size, and percentage.

### /ws/ctrl

**Direction:** Bidirectional

Control endpoint for light, video profile, and video enable/disable. This endpoint has **inline authentication** -- the first message must contain a valid API key (if configured).

**Client-to-server messages:**

```json
{"light": true}
{"light": false}
{"quality": "sd"}
{"quality": "hd"}
{"video": true}
{"video": false}
```

**Server-to-client messages:**

```json
{"light": true, "quality": "hd", "video": true}
```
