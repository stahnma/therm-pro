# Therm-Pro: Remote BBQ Temperature Monitor

## Problem

The ThermPro TP25 is a 4-probe BBQ thermometer with Bluetooth LE connectivity. BLE range is limited to ~30 feet, which means you have to stay near the smoker to monitor temperatures during long cooks. This project extends the TP25's reach over WiFi so you can monitor and receive alerts from anywhere on your network (or beyond, with port forwarding).

## Architecture

Three components:

```
TP25 --BLE--> ESP32 --HTTP POST (WiFi)--> Go Server --WebSocket--> Browser
                                              |
                                              +--Slack Webhook--> Slack
```

### 1. ESP32 BLE Relay

The ESP32's job is intentionally narrow: connect to the TP25, decode BLE data, and forward it over WiFi.

**BLE Connection**
- On boot, scan for the TP25 by its BLE service UUID
- Subscribe to the temperature characteristic notification (the TP25 pushes updates)
- Decode raw bytes into probe temperatures (reference existing reverse-engineering of the TP25 protocol for byte layout)

**Data Forwarding**
- POST a JSON payload to the Go server on each BLE notification (roughly every few seconds)
- Payload: `{ "probes": [{ "id": 1, "temp_f": 252.3 }, ...], "timestamp": "..." }`
- If the server is unreachable, drop the reading and try again next cycle (no buffering)

**WiFi / Networking**
- Uses the ESP32 Arduino WiFi library which includes a DHCP client by default
- WiFi SSID/password and server URL configured via `config.h` at compile time

**OTA Updates**
- On boot, checks `GET /api/firmware/latest` on the Go server for a newer firmware version
- Compares a version integer baked in at compile time
- If newer, downloads the `.bin` and self-flashes via ESP32 HTTP OTA
- Workflow: build with PlatformIO, upload `.bin` to server, ESP32 picks it up automatically

**Status LED**
- Blink while scanning for TP25
- Solid when connected to TP25 and server is reachable

**Tooling**
- Arduino framework via PlatformIO
- NimBLE library (lighter and more reliable than the default ESP32 BLE stack)

### 2. Go Server

A single Go binary. All frontend assets embedded via `embed.FS`.

**Project Structure**
```
cmd/therm-pro-server/main.go    -- entry point
internal/api/                   -- HTTP/WebSocket handlers
internal/cook/                  -- cook session state management
internal/slack/                 -- Slack notification logic
internal/web/                   -- embedded dashboard assets
internal/firmware/              -- OTA firmware serving
```

**Data Ingestion**
- `POST /api/data` -- ESP32 posts probe readings here
- On each reading: update in-memory state, append to session history, broadcast to WebSocket clients, evaluate alert rules

**Cook Session Management**
- All data held in memory in a `CookSession` struct: probe labels, history (slice of timestamped readings), alert configs
- Persisted to a JSON file (e.g., `~/.therm-pro/session.json`) on each update so it survives restarts
- `POST /api/session/reset` -- clears the session (run before each cook)
- `GET /api/session` -- returns full session data

**Alert Configuration**
- `POST /api/alerts` -- set per-probe alerts: target temp, optional high/low range
- Each probe has a label (e.g., "Pit", "Brisket", "Ribs")
- Alerts fire once when crossed, then don't repeat until the condition clears and re-triggers

**Firmware OTA Endpoint**
- `GET /api/firmware/latest` -- returns version info and binary download URL
- `GET /api/firmware/download` -- serves the `.bin` file
- `POST /api/firmware/upload` -- upload a new firmware binary

**API Documentation**
- Handlers annotated with swaggo/swag comments
- OpenAPI/Swagger spec auto-generated at build time via `swag init`
- Swagger UI served at `/api/docs`

**WebSocket**
- `GET /api/ws` -- clients connect here for live updates
- Server pushes each new reading as it arrives

### 3. Web Dashboard

**Serving**
- All assets (HTML, CSS, JS) embedded in the Go binary
- Served at `/`

**Layout (mobile-first)**
- Top: 4 probe cards in a 2x2 grid (single column on narrow screens)
- Each card: probe label, current temp (large font), target temp if set
- Color-coded status: green (normal), amber (approaching target), red (target hit or out of range)
- Below cards: time-series line chart of all 4 probes over the session, color-matched

**Controls**
- Tap a probe card to edit label and set/clear alert thresholds
- "Reset Cook" button with confirmation
- Fahrenheit/Celsius toggle

**Live Updates**
- WebSocket connection to `/api/ws`
- Chart and cards update in real time
- Auto-reconnect with "reconnecting..." banner on disconnect

**Tech**
- Vanilla HTML/CSS/JS (no framework, no build step)
- uPlot (~35KB) for time-series charting
- CSS grid for responsive layout

## Slack Integration

**Setup**
- Slack Incoming Webhook URL configured via environment variable (`THERM_PRO_SLACK_WEBHOOK`) or config file

**Notifications**
- Fires when an alert threshold is crossed
- Message includes the alert (e.g., "Brisket (Probe 2) hit target: 203F") plus current temps for all probes
- De-duplicated: fires once on crossing, resets when temp moves back past threshold by a hysteresis margin (~3F)
- Rate limited: minimum 60 seconds between alerts for the same probe/condition

## Probe Usage Model

- Probe 1: pit/smoker temperature (range alerts for high/low)
- Probes 2-4: meat probes (target temperature alerts)
- All 4 visible on dashboard and API at all times
- Alerts configurable per-probe independently

## Data Persistence

- Current cook session stored in memory, backed by a JSON file
- No database required
- User manually resets session before each cook
- Data survives server restarts via the JSON file

## Out of Scope

- Authentication (runs on local network behind user's firewall)
- Cloud deployment
- Multi-user support
- Historical cook comparison / long-term storage

## Open Questions

- Exact BLE service/characteristic UUIDs for the TP25 (reference community reverse-engineering)
- Byte layout for decoding 4 probe temps from BLE notifications
