# ankerctl Wiki

Welcome to the ankerctl wiki. ankerctl is a Go reimplementation of [ankermake-m5-protocol](https://github.com/ankermake/ankermake-m5-protocol) -- a CLI and web UI for monitoring, controlling, and interfacing with AnkerMake M5 3D printers.

## Why Go?

- **Security** -- Strict type system, no dynamic eval, compiled binary
- **Performance** -- Single binary, fast startup, low memory footprint
- **Deployment** -- ~50 MB Docker image (vs ~300 MB Python), no runtime dependencies except ffmpeg
- **Concurrency** -- Native goroutines for MQTT, PPPP, and WebSocket streams

## Pages

- **[Installation and Configuration](Installation-and-Configuration)** -- Docker, binary, and source installation; printer setup; API key; environment variables
- **[Architecture](Architecture)** -- Package layering, service framework, web layer, Python source mapping
- **[Protocol Details](Protocol-Details)** -- MQTT, PPPP, and crypto protocol documentation
- **[API Reference](API-Reference)** -- Complete REST and WebSocket endpoint reference
- **[Development Guide](Development-Guide)** -- Build commands, git workflow, mandates, contributing
- **[Migration Status](Migration-Status)** -- 16-phase migration plan progress and open items
- **[Troubleshooting](Troubleshooting)** -- Common problems and their solutions

## Quick Start

### Option 1: Download the Binary (fastest)

Download the latest release from [Releases](https://github.com/Django1982/ankerctl_go_remake/releases) (v0.9.24+), make it executable, and run:

```sh
chmod +x ankerctl-linux-amd64
./ankerctl-linux-amd64 webserver --listen 0.0.0.0:4470
```

### Option 2: Docker

```sh
docker run -d \
  --name ankerctl \
  --network host \
  -v ~/.ankerctl:/root/.ankerctl \
  -e ANKERCTL_HOST=0.0.0.0 \
  ghcr.io/django1982/ankerctl:latest
```

### Option 3: Build from Source

```sh
git clone https://github.com/Django1982/ankerctl_go_remake.git
cd ankerctl_go_remake
go build -o ankerctl ./cmd/ankerctl/
./ankerctl webserver --listen 0.0.0.0:4470
```

Navigate to [http://localhost:4470](http://localhost:4470) and log in with your AnkerMake email and password.

## Quick Links

- [GitHub Repository](https://github.com/Django1982/ankerctl_go_remake)
- [Releases](https://github.com/Django1982/ankerctl_go_remake/releases)
- [Docker Image](https://ghcr.io/django1982/ankerctl)
- [Issue Tracker](https://github.com/Django1982/ankerctl_go_remake/issues)
