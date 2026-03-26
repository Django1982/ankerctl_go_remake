# Troubleshooting

## Prerequisites

Before troubleshooting, verify that the following are installed:

- **ankerctl** binary or Docker image (see [Installation and Configuration](Installation-and-Configuration))
- **ffmpeg** -- required for the timelapse feature. Docker images include it. For binary installs, install separately:
  - Debian/Ubuntu: `sudo apt install ffmpeg`
  - macOS: `brew install ffmpeg`
  - Windows: download from [ffmpeg.org](https://ffmpeg.org/download.html)
  - Verify: `ffmpeg -version`

---

## Connection Issues

### Printer not found on LAN

**Symptoms:** PPPP connection fails, no video stream, uploads fail.

**Causes and solutions:**

1. **Printer and server not on same network.** Ensure both are on the same LAN/VLAN. PPPP uses UDP broadcast for discovery on port 32108.

2. **Docker bridge networking.** You must use `network_mode: host` in Docker Compose. Bridge networking blocks UDP broadcasts.

3. **Firewall blocking UDP.** Ensure ports 32108 (discovery) and 32100 (session) are open for UDP traffic on the server.

4. **Printer in sleep mode.** Wake the printer and wait a few seconds for discovery.

5. **Wrong printer selected.** If you have multiple printers, check `PRINTER_INDEX` or use the printer selector in the web UI.

### MQTT connection fails

**Symptoms:** No print status updates, cannot send commands, dashboard shows no data.

**Causes and solutions:**

1. **Config not imported.** Upload your `login.json` or log in via the web UI.

2. **Auth token expired.** Re-import `login.json` or log in again. Tokens expire after extended periods.

3. **Network issues.** MQTT connects to Anker's cloud broker on port 8789 (TLS). Ensure outbound TCP 8789 is not blocked.

4. **Region mismatch.** If you moved to a different region, re-import your config to detect the closest API server.

## Web UI Issues

### Cannot access web UI

1. **Check bind address.** Default is `127.0.0.1` (localhost only). For Docker or remote access, set `ANKERCTL_HOST=0.0.0.0`.

2. **Port conflict.** Default port is 4470. Check if another service is using it: `lsof -i :4470` or `ss -tlnp | grep 4470`.

3. **Firewall.** Ensure TCP 4470 is open if accessing from another machine.

### 401 Unauthorized

1. **API key required.** If an API key is configured, you must authenticate. Append `?apikey=your-key` to the URL in your browser.

2. **Session expired.** Clear cookies and re-authenticate with `?apikey=your-key`.

3. **Wrong key.** Double-check the key in `.env` or config. Keys are case-sensitive, minimum 16 characters.

### Video stream not loading

1. **PPPP not connected.** Check the PPPP connection status indicator in the UI. If disconnected, the video stream is unavailable.

2. **Browser codec support.** The video stream is raw H.264. Some browsers may not support inline H.264 playback. Chrome and Firefox work best.

3. **Stall detection.** If no video frames arrive for 15 seconds, the stream is considered stalled. Reload the page to reconnect.

## Print Issues

### Upload fails / times out

1. **File too large.** Check `UPLOAD_MAX_MB` (default 2048 MB). Increase if your G-code files are larger.

2. **PPPP not connected.** File transfer uses the PPPP protocol. Ensure LAN connectivity to the printer.

3. **Upload rate too high.** If uploads fail or corrupt, try reducing `UPLOAD_RATE_MBPS` (default 10). Lower values (5) are more reliable on congested networks.

### Print does not start after upload

1. **"Send and Print" required.** ankerctl sends the file and immediately starts printing. There is no "upload only" queue on the printer.

2. **Printer busy.** If a print is already running, the upload will queue but not start. Pause or cancel the current print first.

### GCode not recognized

1. **Slicer compatibility.** ankerctl works best with PrusaSlicer, OrcaSlicer, SuperSlicer, and Bamboo Studio. Other slicers may produce G-code that the printer does not accept.

2. **File encoding.** Ensure the G-code file is UTF-8 encoded with Unix line endings (LF, not CRLF).

## Timelapse Issues

### Timelapse not recording

1. **Feature disabled.** Set `TIMELAPSE_ENABLED=true` in `.env` or enable via Setup tab.

2. **ffmpeg not installed.** Timelapse requires ffmpeg for video encoding. In Docker, it is included. For binary installs, install it separately and verify with `ffmpeg -version`. See the [Prerequisites](#prerequisites) section above.

3. **No active print.** Timelapse only records during prints. It starts automatically when a print begins and stops when it ends.

### Timelapse video is empty or very short

1. **Camera not streaming.** If the video stream is unavailable (PPPP disconnected), no frames are captured.

2. **Interval too long.** For short prints, a 30-second interval may capture very few frames. Reduce `TIMELAPSE_INTERVAL_SEC`.

3. **Resume window.** If a print is paused and resumed within 60 minutes, frames are seamlessly appended. If resumed after 60 minutes, a new timelapse starts.

## Notification Issues

### Apprise notifications not arriving

1. **Apprise server unreachable.** Verify `APPRISE_SERVER_URL` points to a running Apprise API server.

2. **Wrong key.** Check `APPRISE_KEY` matches your Apprise configuration.

3. **Events disabled.** Ensure the specific event toggle is set to `true` (e.g., `APPRISE_EVENT_PRINT_FINISHED=true`).

4. **Test first.** Use Setup > Notifications > Send Test to verify the connection.

## Home Assistant Issues

### Entities not appearing in HA

1. **MQTT broker.** Ensure `HA_MQTT_HOST` and `HA_MQTT_PORT` point to your MQTT broker (e.g., Mosquitto).

2. **Discovery prefix.** Default is `homeassistant`. If your HA uses a different prefix, set `HA_MQTT_DISCOVERY_PREFIX`.

3. **Restart HA.** After enabling ankerctl HA integration, restart Home Assistant to pick up the discovery payloads.

4. **MQTT integration.** Ensure the MQTT integration is installed and configured in Home Assistant.

## Docker Issues

### Container keeps restarting

1. **Config directory.** Ensure the volume mount for `~/.ankerctl` exists and is writable.

2. **Check logs:** `docker logs ankerctl`

3. **Port conflict.** With `network_mode: host`, port 4470 must be available on the host.

### Permission denied on config

1. **Volume ownership.** The config directory must be readable/writable by the container user. Check permissions: `ls -la ~/.ankerctl/`.

2. **SELinux.** On SELinux-enabled systems, add `:z` to volume mounts: `-v ~/.ankerctl:/root/.ankerctl:z`.

## Debug Mode

Enable debug mode for detailed diagnostics:

```sh
ANKERCTL_DEV_MODE=true ./ankerctl webserver
```

This enables:
- Debug tab in the web UI
- State inspector (live JSON dump)
- Service health panel
- Event simulation
- Log viewer
- `/api/debug/*` endpoints

> **Warning:** Do not enable in production. The debug tab exposes internal state.

## Log Files

Set `ANKERCTL_LOG_DIR` to enable file logging:

```sh
ANKERCTL_LOG_DIR=/var/log/ankerctl ./ankerctl webserver
```

Log files can be viewed in the Debug tab (when dev mode is enabled) or directly on disk.

## Getting Help

1. Check the [GitHub Issues](https://github.com/Django1982/ankerctl_go_remake/issues) for known problems
2. Open a new issue with:
   - ankerctl version (`./ankerctl version`)
   - Printer model and firmware version
   - Relevant log output (redact sensitive data)
   - Steps to reproduce
