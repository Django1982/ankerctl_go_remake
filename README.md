# ankerctl (Go)

A Go reimplementation of [ankerctl](https://github.com/ankermake/ankermake-m5-protocol) -- a CLI and web UI tool for monitoring, controlling, and interfacing with AnkerMake M5 3D printers.

## Why Go?

- **Security**: Strict type system, no dynamic eval, compiled binary
- **Performance**: Single binary, fast startup, low memory footprint
- **Deployment**: ~50MB Docker image (vs ~300MB Python), no runtime dependencies except ffmpeg
- **Concurrency**: Native goroutines for MQTT, PPPP, and WebSocket streams

## Status

Migration progressed beyond scaffold stage. Core layers (config/model, crypto, MQTT/PPPP,
service framework, HTTP middleware/server, major HTTP handlers, WebSockets, and
notifications) are implemented with tests.

See [MIGRATION_PLAN.md](MIGRATION_PLAN.md) for the roadmap and
`docs/agents/reports/` for phase completion/review notes.

## Project Structure

```
cmd/ankerctl/           CLI entry point
internal/
  config/               Configuration management
  crypto/               AES-256-CBC, ECDH, checksums
  mqtt/protocol/        MQTT message types and packet structures
  mqtt/client/          MQTT client (Anker broker connection)
  pppp/protocol/        PPPP packet types (UDP P2P)
  pppp/client/          PPPP API, channels, file transfer
  pppp/crypto/          PPPP-specific crypto (curse/decurse)
  httpapi/              Anker Cloud HTTP API client
  service/              Service framework + all background services
  web/                  HTTP server, routes, templates
  web/handler/          HTTP handler functions (grouped by feature)
  web/ws/               WebSocket endpoint handlers
  web/middleware/        Auth, security headers, rate limiting
  notifications/        Apprise notification client
  gcode/                GCode parsing (time patching, layer count)
  model/                Data models (Config, Account, Printer)
  util/                 Shared utilities
  logging/              Structured logging setup
static/                 Frontend files (HTML/JS/CSS, unchanged from Python)
```

## Building

```bash
go build -o ankerctl ./cmd/ankerctl/
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/go-chi/chi/v5` | HTTP router |
| `github.com/gorilla/websocket` | WebSocket |
| `modernc.org/sqlite` | SQLite (CGO-free) |
| `github.com/eclipse/paho.mqtt.golang` | MQTT client |
| `github.com/spf13/cobra` | CLI framework |
| `github.com/google/uuid` | UUID generation |

## License

GPLv3 -- see [LICENSE](LICENSE)
