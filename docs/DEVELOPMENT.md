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
# export ESP32_LED_PIN=2                # 8 for ESP32-C3 DevKitM-1

# Optional BLE overrides (default to legacy TP25 if unset):
# export ESP32_BLE_NAME="TP25"
# export ESP32_BLE_SERVICE_UUID="1086fff0-3343-4817-8bb2-b32206336ce8"
# export ESP32_BLE_WRITE_CHAR_UUID="1086fff1-3343-4817-8bb2-b32206336ce8"
# export ESP32_BLE_NOTIFY_CHAR_UUID="1086fff2-3343-4817-8bb2-b32206336ce8"
```

Step-by-step build commands (substitute `esp32c3-` for `esp32-` to target the ESP32-C3 DevKitM-1):

```bash
make esp32-scan      # BLE-discover the Therm-Pro unit, write esp32/.ble-config
make esp32-config    # Generate config.h from env vars + .ble-config
make esp32-build     # Compile firmware
make esp32-flash     # Build + flash via USB
make esp32-monitor   # Monitor serial output

make esp32-all       # All of the above (scan → config → build → flash)
```

`esp32-config` always re-reads `esp32/.ble-config` if it exists; shell env vars take precedence so you can override individual BLE fields ad hoc. With no overrides and no `.ble-config`, the legacy TP25 defaults are baked in.

Flashing uses [espflash](https://github.com/esp-rs/espflash) (a Rust-based flasher provided by Flox) instead of PlatformIO's esptool, which avoids pyserial compatibility issues under nix.

### BLE discovery (`therm-pro-scan`)

`cmd/therm-pro-scan` is a small Go BLE central used by `make esp32-scan` to figure out the right name + UUIDs for a given Therm-Pro unit. It depends on `tinygo.org/x/bluetooth`, which on macOS uses CoreBluetooth via cgo (so `CGO_ENABLED=1` is required and the Makefile sets it).

Modes:

- `-probe` (default in `make esp32-scan`): scan, drop obvious consumer-brand noise, connect to each survivor, classify by service shape. HIGH if the device exposes the TP25 service UUID `1086fff0-…`; MEDIUM if it has a custom 128-bit service with a notify+write characteristic pair. Short-circuits on the first HIGH match.
- `-name <substring>`: legacy filter — match by advertised local name (case-insensitive substring).
- `-diff`: interactive two-pass scan. Takes a baseline, prompts for power-cycle, takes a second scan, reports devices that newly appeared.
- `-list`: dump everything seen during the scan window, with advertised service UUIDs and manufacturer data.

A successful probe writes `esp32/.ble-config` (gitignored) including `ESP32_BLE_ADDRESS` — the macOS CoreBluetooth UUID for the device. On the next run the scanner takes a fast path: it connects directly to the cached address and skips the scan entirely if the TP25 service is still present. If the device is gone (powered off, replaced, etc.) the direct connect times out and the code falls through to a full scan, no manual cache invalidation needed.

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
