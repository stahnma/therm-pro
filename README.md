# Therm-Pro

Remote BBQ temperature monitoring system for the ThermPro TP25. An ESP32 connects to the TP25 over Bluetooth LE and relays temperature data over WiFi to a Go server, which provides a real-time web dashboard and Slack alerts.

```
TP25 --BLE--> ESP32 --HTTP/WiFi--> Go Server --WebSocket--> Browser
                                       |
                                       +--Webhook--> Slack
```

## Features

- **4 probe support** -- pit temp + 3 meat probes, all tracked independently
- **Real-time web dashboard** -- mobile-friendly dark theme, live-updating probe cards and time-series chart
- **Slack alerts** -- notifications when target temps are hit or pit temp drifts out of range
- **Alert hysteresis** -- alerts fire once, reset after 3 degrees F, rate-limited to avoid spam
- **OTA firmware updates** -- upload new ESP32 firmware via the server, ESP32 pulls it on boot
- **Session persistence** -- cook data survives server restarts, manual reset between cooks

## Prerequisites

- **Go 1.21+** -- for building the server
- **PlatformIO** -- for building and flashing the ESP32 firmware
- **ESP32 dev board** -- any ESP32 with WiFi and BLE (e.g., ESP32-DevKitC)
- **ThermPro TP25** -- the Bluetooth BBQ thermometer

### Installing PlatformIO

```bash
# Via pip
pip install platformio

# Or via Homebrew (macOS)
brew install platformio
```

## Quick Start

### 1. Build and Run the Server

```bash
# Clone the repo
git clone https://github.com/stahnma/therm-pro.git
cd therm-pro

# Build
make build

# Run (listens on port 8080 by default)
./bin/therm-pro-server
```

The server stores session data in `~/.therm-pro/session.json`.

**Configuration via environment variables:**

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP server port |
| `THERM_PRO_SLACK_WEBHOOK` | _(empty)_ | Slack incoming webhook URL for alerts |

Example with Slack:

```bash
THERM_PRO_SLACK_WEBHOOK="https://hooks.slack.com/services/T.../B.../..." ./bin/therm-pro-server
```

### 2. Configure and Flash the ESP32

Edit `esp32/src/config.h` with your network details:

```cpp
#define WIFI_SSID "your-wifi-name"
#define WIFI_PASS "your-wifi-password"
#define SERVER_URL "http://192.168.1.100:8080"  // your server's IP
#define FIRMWARE_VERSION 1
#define LED_PIN 2  // onboard LED pin (2 for most ESP32 dev boards)
```

Build and flash:

```bash
cd esp32

# Build only (verify it compiles)
pio run

# Build and flash via USB
pio run -t upload

# Monitor serial output (optional, useful for debugging)
pio device monitor
```

**Finding your server's IP:**

```bash
# macOS
ipconfig getifaddr en0

# Linux
hostname -I | awk '{print $1}'
```

### 3. Open the Dashboard

Open `http://<server-ip>:8080` in a browser. You should see 4 probe cards updating in real time once the ESP32 connects to the TP25.

## Usage

### Setting Up a Cook

1. Turn on your TP25 and insert probes
2. Power on the ESP32 -- it will auto-connect to the TP25 (LED blinks while scanning, solid when connected)
3. Open the dashboard on your phone or laptop
4. Tap each probe card to set a label (e.g., "Pit", "Brisket") and alert thresholds
5. Cook!

### Alert Types

| Alert | Use Case | Example |
|-------|----------|---------|
| **Target temp** | Meat is done | Brisket probe hits 203 F |
| **High temp** | Pit running hot | Pit temp exceeds 275 F |
| **Low temp** | Pit running cold / fire dying | Pit temp drops below 225 F |

Alerts fire once when the threshold is crossed, then reset after the temperature moves 3 degrees F past the threshold (hysteresis). Minimum 60 seconds between repeated alerts for the same probe.

### Resetting Between Cooks

Click the "Reset Cook" button on the dashboard (or `POST /api/session/reset`) to clear all temperature history. Probe labels and alert configurations are preserved.

## API

All endpoints are available at `http://<server-ip>:8080`.

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/` | Web dashboard |
| `GET` | `/healthz` | Health check |
| `POST` | `/api/data` | Submit probe readings (ESP32 uses this) |
| `GET` | `/api/session` | Get current cook session |
| `POST` | `/api/session/reset` | Reset cook session |
| `POST` | `/api/alerts` | Set alert config for a probe |
| `GET` | `/api/ws` | WebSocket for live updates |
| `GET` | `/api/firmware/latest` | Check latest firmware version |
| `GET` | `/api/firmware/download` | Download firmware binary |
| `POST` | `/api/firmware/upload` | Upload new firmware binary |

### Example: Submit Temperature Data

```bash
curl -X POST http://localhost:8080/api/data \
  -H 'Content-Type: application/json' \
  -d '{
    "probes": [
      {"id": 1, "temp_f": 250.0},
      {"id": 2, "temp_f": 165.3},
      {"id": 3, "temp_f": 180.1},
      {"id": 4, "temp_f": -999.0}
    ],
    "battery": 85
  }'
```

A `temp_f` of `-999.0` indicates a disconnected probe.

### Example: Set Alert

```bash
curl -X POST http://localhost:8080/api/alerts \
  -H 'Content-Type: application/json' \
  -d '{"probe_id": 2, "alert": {"target_temp": 203.0}}'
```

```bash
curl -X POST http://localhost:8080/api/alerts \
  -H 'Content-Type: application/json' \
  -d '{"probe_id": 1, "alert": {"low_temp": 225.0, "high_temp": 275.0}}'
```

## OTA Firmware Updates

After the initial USB flash, you can update the ESP32 over WiFi:

1. Make your code changes in `esp32/src/`
2. Increment `FIRMWARE_VERSION` in `esp32/src/config.h`
3. Build: `cd esp32 && pio run`
4. Upload the binary to the server:

```bash
curl -X POST http://localhost:8080/api/firmware/upload \
  -F "firmware=@esp32/.pio/build/esp32/firmware.bin" \
  -F "version=2"
```

5. Reboot the ESP32 (power cycle or reset button) -- it checks for updates on boot and will self-flash

## ESP32 LED Status

| LED State | Meaning |
|-----------|---------|
| Blinking | Connecting to WiFi or scanning for TP25 |
| Solid on | Connected to TP25 and sending data |
| Off | BLE disconnected, attempting reconnect |

## Network Setup

The server runs on your local network. The ESP32 and your phone/laptop need to be on the same network (or have routes to the server).

**Accessing from outside your network:** Set up port forwarding on your router to forward an external port to `<server-ip>:8080`. The specifics depend on your router.

## Project Structure

```
therm-pro/
  cmd/therm-pro-server/     Go server entry point
  internal/
    api/                     HTTP handlers, WebSocket, routes
    cook/                    Session data model, alerts, persistence
    firmware/                OTA firmware management
    slack/                   Slack webhook client
    web/static/              Embedded dashboard (HTML/CSS/JS)
  esp32/
    src/                     ESP32 firmware (Arduino/C++)
    platformio.ini           PlatformIO build config
```

## Running Tests

```bash
go test ./...
```

## Development

You can develop and test the server without an ESP32 by simulating probe data with curl:

```bash
# Start the server
make run

# In another terminal, simulate temperature readings
while true; do
  curl -s -X POST http://localhost:8080/api/data \
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

## License

MIT
