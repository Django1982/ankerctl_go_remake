# API Reference

## Overview

ankerctl exposes a REST API and WebSocket streams on `127.0.0.1:4470` (default).

## Authentication Rules

- **POST/DELETE**: Always require API key (`X-Api-Key` header or `apikey` query param)
- **GET**: Public by default, except protected paths
- **Protected GET paths**:
  - `/api/ankerctl/server/reload`
  - `/api/debug/*`
  - `/api/settings/mqtt`
  - `/api/notifications/settings`
  - `/api/printers`
  - `/api/history`
- **Exempt when no printer configured**:
  - `/api/ankerctl/config/upload`
  - `/api/ankerctl/config/login`

## REST Endpoints

See individual endpoint documentation:

- [General](endpoints/general.md) -- Health, version, index page
- [Config](endpoints/config.md) -- Config upload, reload, login
- [Printer](endpoints/printer.md) -- Printer status, control, snapshot
- [History](endpoints/history.md) -- Print history
- [Timelapse](endpoints/timelapse.md) -- Timelapse videos
- [Filament](endpoints/filament.md) -- Filament profiles
- [Notifications](endpoints/notifications.md) -- Apprise notification settings
- [Settings](endpoints/settings.md) -- MQTT and app settings
- [OctoPrint](endpoints/octoprint.md) -- OctoPrint compatibility layer

## WebSocket Streams

| Path | Direction | Content | Auth |
|---|---|---|---|
| `/ws/mqtt` | Server -> Client | JSON MQTT events | No |
| `/ws/video` | Server -> Client | Binary H.264 frames | No |
| `/ws/pppp-state` | Server -> Client | JSON connection state (polled) | No |
| `/ws/upload` | Server -> Client | JSON upload progress | No |
| `/ws/ctrl` | Bidirectional | JSON commands | Inline (first message) |
