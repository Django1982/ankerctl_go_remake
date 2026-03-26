# Installation and Configuration

## Docker (recommended)

The Docker image is published to `ghcr.io/django1982/ankerctl`. It includes `ffmpeg` for timelapse support and runs as a single static binary.

### Quick Start

```sh
docker run -d \
  --name ankerctl \
  --network host \
  -v ~/.ankerctl:/root/.ankerctl \
  -e ANKERCTL_HOST=0.0.0.0 \
  ghcr.io/django1982/ankerctl:latest
```

### Docker Compose

```yaml
services:
  ankerctl:
    image: ghcr.io/django1982/ankerctl:latest
    container_name: ankerctl
    network_mode: host
    restart: unless-stopped
    volumes:
      - ~/.ankerctl:/root/.ankerctl
      - captures:/captures
    env_file: .env

volumes:
  captures:
```

Copy `.env.example` to `.env`, adjust the values, then run `docker compose up -d`.

> **Important:** `network_mode: host` is required because the PPPP protocol uses UDP peer-to-peer communication on the local network. Bridge networking will not work.

### Updating

```sh
docker compose pull
docker compose up -d
```

## Binary Download

Download the latest release for your platform from [Releases](https://github.com/Django1982/ankerctl_go_remake/releases).

Available binaries:
- `ankerctl-linux-amd64`
- `ankerctl-linux-arm64`
- `ankerctl-darwin-amd64` (Intel Mac)
- `ankerctl-darwin-arm64` (Apple Silicon)
- `ankerctl-windows-amd64.exe`

```sh
# Linux / macOS
chmod +x ankerctl-linux-amd64
./ankerctl-linux-amd64 webserver --listen 0.0.0.0:4470
```

> **Note:** For timelapse support, install `ffmpeg` separately (`apt install ffmpeg`, `brew install ffmpeg`, etc.).

## From Source

Requires Go 1.22 or later.

```sh
git clone https://github.com/Django1982/ankerctl_go_remake.git
cd ankerctl_go_remake
go build -o ankerctl ./cmd/ankerctl/
./ankerctl webserver
```

## Printer Setup

### Login with Anker Account (recommended)

The simplest way to set up your printer is to log in with your AnkerMake account credentials.

**Via Web UI:**

1. Start ankerctl (binary, Docker, or source)
2. Open [http://localhost:4470](http://localhost:4470)
3. On the Setup tab, enter your AnkerMake email and password

**Via CLI:**

```sh
./ankerctl config login DE
```

You will be prompted for email and password. Replace `DE` with your country code.

> **Note:** Legacy alternative -- if you already have a `login.json` file from the AnkerMake desktop app, you can import it via CLI as a backup method:
>
> ```sh
> ./ankerctl config import path/to/login.json
> ```
>
> The `login.json` file can be found at:
>
> | Platform | Path |
> |----------|------|
> | macOS | `~/Library/Application Support/AnkerMake/AnkerMake_64bit_fp/login.json` |
> | Windows | `%APPDATA%\Roaming\eufyMake Studio Profile\cache\offline\user_info` |
> | Linux (Wine) | `~/.wine/drive_c/users/<name>/AppData/Roaming/eufyMake Studio Profile/cache/offline/user_info` |

## API Key

API key authentication protects all write operations (uploading files, sending G-code, controlling the printer). Read operations (status, video stream) remain public.

```sh
# Generate a random key
./ankerctl config set-password

# Set a specific key (minimum 16 characters, [a-zA-Z0-9_-])
./ankerctl config set-password my-secret-key

# Remove key (disable authentication)
./ankerctl config remove-password
```

For Docker, set `ANKERCTL_API_KEY` in your `.env` file.

**Using the key with slicers:**
In PrusaSlicer, OrcaSlicer, or similar, configure a printer with host `localhost`, port `4470`, and enter the key as the API Key. The slicer sends it as `X-Api-Key` header.

**Using the key in the browser:**
Append `?apikey=your-key` to the URL once. A session cookie is set automatically.

## Environment Variables

See [`.env.example`](https://github.com/Django1982/ankerctl_go_remake/blob/main/.env.example) for a complete template with comments. All variables are optional -- ankerctl works with sensible defaults.

### Server

| Variable | Default | Description |
|----------|---------|-------------|
| `ANKERCTL_HOST` / `FLASK_HOST` | `127.0.0.1` | Bind address |
| `ANKERCTL_PORT` / `FLASK_PORT` | `4470` | Listen port |
| `FLASK_SECRET_KEY` | *(auto)* | Session cookie secret |
| `PRINTER_INDEX` | `0` | Active printer index (0-based, only needed for multi-printer setups) |

### Upload

| Variable | Default | Description |
|----------|---------|-------------|
| `UPLOAD_MAX_MB` | `2048` | Max upload size in MB |
| `UPLOAD_RATE_MBPS` | `10` | Upload speed (Mbit/s): 5, 10, 25, 50, 100 |

### Security

| Variable | Default | Description |
|----------|---------|-------------|
| `ANKERCTL_API_KEY` | *(unset)* | API key for write operations |
| `ANKERCTL_DEV_MODE` | `false` | Enable debug endpoints |
| `ANKERCTL_LOG_DIR` | *(unset)* | Log file directory |

### Notifications (Apprise)

| Variable | Default | Description |
|----------|---------|-------------|
| `APPRISE_ENABLED` | `false` | Enable push notifications |
| `APPRISE_SERVER_URL` | *(unset)* | Apprise API server URL |
| `APPRISE_KEY` | *(unset)* | Notification key/ID |
| `APPRISE_TAG` | *(unset)* | Tag filter |
| `APPRISE_EVENT_PRINT_STARTED` | `true` | Notify on print start |
| `APPRISE_EVENT_PRINT_FINISHED` | `true` | Notify on print finish |
| `APPRISE_EVENT_PRINT_FAILED` | `true` | Notify on print failure |
| `APPRISE_EVENT_GCODE_UPLOADED` | `true` | Notify on G-code upload |
| `APPRISE_EVENT_PRINT_PROGRESS` | `true` | Notify on progress updates |
| `APPRISE_PROGRESS_INTERVAL` | `25` | Progress interval (%) |
| `APPRISE_PROGRESS_INCLUDE_IMAGE` | `false` | Attach camera snapshot |
| `APPRISE_SNAPSHOT_QUALITY` | `hd` | Snapshot quality: sd, hd, fhd |
| `APPRISE_SNAPSHOT_FALLBACK` | `true` | Use G-code preview if live fails |
| `APPRISE_SNAPSHOT_LIGHT` | `false` | Turn on light for snapshot |

### Print History

| Variable | Default | Description |
|----------|---------|-------------|
| `PRINT_HISTORY_RETENTION_DAYS` | `90` | Days to keep history |
| `PRINT_HISTORY_MAX_ENTRIES` | `500` | Max history entries |

### Timelapse

| Variable | Default | Description |
|----------|---------|-------------|
| `TIMELAPSE_ENABLED` | `false` | Enable timelapse capture |
| `TIMELAPSE_INTERVAL_SEC` | `30` | Seconds between frames |
| `TIMELAPSE_MAX_VIDEOS` | `10` | Max videos to keep |
| `TIMELAPSE_SAVE_PERSISTENT` | `true` | Save videos persistently |
| `TIMELAPSE_CAPTURES_DIR` | `/captures` | Video storage directory |
| `TIMELAPSE_LIGHT` | *(unset)* | Light mode: `snapshot` or `session` |

### Home Assistant

| Variable | Default | Description |
|----------|---------|-------------|
| `HA_MQTT_ENABLED` | `false` | Enable HA MQTT Discovery |
| `HA_MQTT_HOST` | `localhost` | MQTT broker host |
| `HA_MQTT_PORT` | `1883` | MQTT broker port |
| `HA_MQTT_USER` | *(unset)* | MQTT username |
| `HA_MQTT_PASSWORD` | *(unset)* | MQTT password |
| `HA_MQTT_DISCOVERY_PREFIX` | `homeassistant` | HA discovery prefix |
| `HA_MQTT_TOPIC_PREFIX` | `ankerctl` | State topic prefix |

## Slicer Setup (PrusaSlicer / OrcaSlicer)

1. In your slicer, add a new printer or edit the physical printer settings
2. Set the printer type to "OctoPrint" or generic HTTP
3. Enter:
   - **Host:** `localhost` (or your server IP)
   - **Port:** `4470`
   - **API Key:** your configured key (if set)
4. Use "Send and Print" to upload and immediately start printing

ankerctl implements the OctoPrint-compatible `/api/files/local` endpoint for file uploads.

## Multiple Printers

If you have multiple AnkerMake printers configured, you can select the active one with `PRINTER_INDEX`. Defaults to 0 (first printer), so this is only needed if you want to use a different printer.

- **Environment variable:** `PRINTER_INDEX=1`
- **CLI flag:** `./ankerctl webserver --printer-index 1`
- **Web UI:** Use the printer selector in the top navigation
