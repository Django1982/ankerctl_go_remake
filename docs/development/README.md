# Development Guide

## Prerequisites

- Go 1.22 or later
- Git
- Docker (optional, for containerized builds)
- ffmpeg (optional, for timelapse feature)

## Getting Started

```bash
# Clone the repository
git clone https://github.com/django1982/ankerctl.git
cd ankerctl

# Build
go build -o ankerctl ./cmd/ankerctl/

# Run tests
go test ./...

# Lint
go vet ./...
```

## Project Layout

```
cmd/ankerctl/       Entry point (main.go, cobra CLI)
internal/           All internal packages (not importable by external code)
  config/           Config file management
  model/            Core data structures
  crypto/           AES-256-CBC, ECDH
  mqtt/             MQTT protocol and client
  pppp/             PPPP protocol, client, and crypto
  httpapi/          Anker cloud HTTP API
  service/          Service framework and all services
  web/              HTTP server, handlers, WebSocket, middleware
  notifications/    Apprise integration
  gcode/            GCode parsing utilities
  util/             Shared helpers
  logging/          Structured logging setup
static/             Frontend assets (HTML/JS/CSS, unchanged from Python)
docs/               This documentation
```

## Configuration

Config file location: `~/.config/ankerctl/default.json`

The config directory is created with permissions `0700`. Set up a printer by
running `ankerctl config import` or uploading via the web UI.

## Environment Variables

| Variable | Default | Description |
|---|---|---|
| `FLASK_HOST` / `ANKERCTL_HOST` | `127.0.0.1` | HTTP bind address |
| `FLASK_PORT` / `ANKERCTL_PORT` | `4470` | HTTP bind port |
| `ANKERCTL_API_KEY` | (none) | API key for protected endpoints |
| `ANKERCTL_DEV_MODE` | `false` | Enable debug endpoints |
| `PRINTER_INDEX` | `0` | Active printer index |

See `CLAUDE.md` for the full list.

## Testing

```bash
# All tests
go test ./...

# Specific package
go test ./internal/config/...

# Verbose with coverage
go test -v -cover ./internal/model/...

# Single test
go test -run TestConfigLoad ./internal/config/
```

## Docker

```bash
# Build image
docker build -t ankerctl .

# Run with host networking (required for PPPP UDP)
docker run --network host ankerctl
```

## Contributing

1. Follow Go conventions: `gofmt`, `go vet`, meaningful error messages
2. Respect the package dependency graph (see architecture docs)
3. Never import upward in the dependency chain
4. Write tests for all new functionality
5. Keep security requirements in mind (see `CLAUDE.md`)
