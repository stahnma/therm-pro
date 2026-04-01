# Development Guide

## Development Environment

This project uses [Flox](https://flox.dev) for dependency management. Flox provides Go, PlatformIO, and GNU Make in an isolated environment.

```bash
# Install Flox (https://flox.dev/docs/install)
# macOS:
brew install flox

# Activate the environment (installs Go, PlatformIO, espflash, GNU Make)
flox activate
```

Verify tools are available:

```bash
go version         # Go compiler
pio --version      # PlatformIO (build)
espflash --version # ESP32 flasher
make --version     # GNU Make
```

### Building Without Flox

If you prefer not to use Flox, install the dependencies manually:

- [Go 1.21+](https://go.dev/dl/)
- [PlatformIO Core](https://docs.platformio.org/en/latest/core/installation.html)
- GNU Make

Then use the same `make build`, `pio run`, etc. commands.

## Project Structure

```
therm-pro/
  .flox/                         Flox environment (Go, PlatformIO, Make)
  contrib/                       systemd unit file, packaging helpers
  cmd/therm-pro-server/          Go server entry point
  internal/
    api/                         HTTP handlers, WebSocket, routes
    consul/                      Consul service registration
    cook/                        Session data model, alerts, persistence
    firmware/                    OTA firmware management
    chart/                       Server-side chart rendering (PNG)
    slack/                       Slack webhook client, slash command handler
    web/static/                  Embedded dashboard (HTML/CSS/JS)
  esp32/
    src/                         ESP32 firmware (Arduino/C++)
    platformio.ini               PlatformIO build config
  docs/plans/                    Design and implementation docs
```

## Running Tests

```bash
go test ./...
```

## Simulating the ESP32

You can develop and test the server without hardware by sending fake probe data with curl:

```bash
# Start the server
make run

# In another terminal, simulate temperature readings
while true; do
  curl -s -X POST http://localhost:8088/api/data \
    -H 'Content-Type: application/json' \
    -d "{\"probes\":[
      {\"id\":1,\"temp_f\":$(shuf -i 240-260 -n 1)},
      {\"id\":2,\"temp_f\":$(shuf -i 160-205 -n 1)},
      {\"id\":3,\"temp_f\":$(shuf -i 170-195 -n 1)},
      {\"id\":4,\"temp_f\":-999}
    ],\"battery\":85}"
  sleep 3
done
```

## ESP32 Build Details

Configuration is driven by environment variables so that secrets are never committed to git:

```bash
export ESP32_WIFI_SSID="your-wifi-name"
export ESP32_WIFI_PASS="your-wifi-password"

# Optional (these have defaults):
# export ESP32_SERVER_URL="http://tp25.service.dc1.consul:8088"
# export ESP32_FIRMWARE_VERSION=1
# export ESP32_LED_PIN=2
```

Step-by-step build commands:

```bash
make esp32-config    # Generate config.h from env vars
make esp32-build     # Compile firmware
make esp32-flash     # Build + flash via USB
make esp32-monitor   # Monitor serial output
```

Flashing uses [espflash](https://github.com/esp-rs/espflash) (a Rust-based flasher provided by Flox) instead of PlatformIO's esptool, which avoids pyserial compatibility issues under nix.

### PlatformIO build fails under Flox

If the ESP32 toolchain fails to install, try `pio pkg install` separately first.

## API Reference

All endpoints are available at `http://<server-ip>:8088`.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/` | Web dashboard |
| `GET` | `/healthz` | Health check (liveness probe for Consul) |
| `GET` | `/api/docs` | API documentation page |
| `GET` | `/diagnostics` | System diagnostics and connectivity status |
| `POST` | `/api/data` | Submit probe readings (ESP32 uses this) |
| `GET` | `/api/session` | Get current cook session |
| `POST` | `/api/session/reset` | Reset cook session |
| `POST` | `/api/alerts` | Set alert config for a probe |
| `GET` | `/api/ws` | WebSocket for live updates |
| `GET` | `/api/firmware/latest` | Check latest firmware version |
| `GET` | `/api/firmware/download` | Download firmware binary |
| `POST` | `/api/firmware/upload` | Upload new firmware binary |
| `POST` | `/slack/command` | Slack slash command endpoint (requires signing secret) |

### Example: Submit Temperature Data

```bash
curl -X POST http://localhost:8088/api/data \
  -H 'Content-Type: application/json' \
  -d '{
    "probes": [
      {"id": 1, "temp_f": 250.0},
      {"id": 2, "temp_f": 165.3},
      {"id": 3, "temp_f": 180.1},
      {"id": 4, "temp_f": -999.0}
    ],
    "battery": 85,
    "firmware_version": 2,
    "ble_connected": true
  }'
```

A `temp_f` of `-999.0` indicates a disconnected probe. The `firmware_version` and `ble_connected` fields are optional and used by the `/diagnostics` endpoint to report ESP32 status.

### Example: Set Alert

```bash
# Target temperature alert (meat probe)
curl -X POST http://localhost:8088/api/alerts \
  -H 'Content-Type: application/json' \
  -d '{"probe_id": 2, "alert": {"target_temp": 203.0}}'
```

```bash
# Range alert (pit probe)
curl -X POST http://localhost:8088/api/alerts \
  -H 'Content-Type: application/json' \
  -d '{"probe_id": 1, "alert": {"low_temp": 225.0, "high_temp": 275.0}}'
```

## BLE Protocol

The ThermoPro TP25 BLE protocol (UUIDs, handshake, notification frame format, BCD temperature encoding, and sentinel values) is documented in [protocol.md](protocol.md).
