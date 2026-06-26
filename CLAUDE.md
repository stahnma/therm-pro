# CLAUDE.md

Guidance for working in this repository.

## What this is

Remote BBQ temperature monitoring for the ThermoPro TP25. An ESP32 reads the
TP25 over Bluetooth LE and relays data over WiFi/HTTP to a Go server, which
serves a real-time web dashboard (WebSocket) and sends Slack alerts.

```
TP25 --BLE--> ESP32 --HTTP/WiFi--> Go Server --WebSocket--> Browser
                                       |
                                       +--Webhook--> Slack
```

Module: `github.com/stahnma/therm-pro` (Go 1.25+).

Layout:
- `cmd/therm-pro-server/` — Go server entrypoint
- `internal/` — server packages
- `esp32/` — PlatformIO firmware (`src/`, `platformio.ini`); two boards: `env:esp32` and `env:esp32c3`
- `docs/` — development and design docs
- `contrib/` — supporting files

## Environment: use Flox

All toolchains (Go, GNU Make, PlatformIO, espflash) are provided by the Flox
environment in `.flox/`. **Do not install these globally or assume they are on
the PATH.** Run commands inside the environment:

```bash
flox activate            # enter a subshell with the toolchain on PATH
# ...then run make/go/pio/espflash as normal

flox activate -- make test   # or run a single command without a subshell
```

To add a tool, edit `.flox/env/manifest.toml` (`[install]` section) or run
`flox install <pkg>` — don't reach for Homebrew/apt/global `go install`.

## Build & tasks: use Make

Drive everything through the `Makefile` rather than calling `go`/`pio`/`espflash`
directly — the targets set the right flags (`CGO_ENABLED`, ldflags, board env)
and ordering. Run `make help` to list targets.

### Go server
```bash
make build     # build bin/therm-pro-server (CGO_ENABLED=0, stamps GitCommit)
make run       # run the server without building
make test      # go test ./...
make fmt       # go fmt ./...
make tidy      # go mod tidy
make clean     # remove bin/
```

### ESP32 firmware
Firmware config (`esp32/src/config.h`) is generated from env vars and is
gitignored; see `esp32/src/config.h.example`. Required before building:

```bash
export ESP32_WIFI_SSID="your-ssid"
export ESP32_WIFI_PASS="your-wifi-password"
# Optional: ESP32_SERVER_URL, ESP32_FIRMWARE_VERSION, ESP32_LED_PIN
```

ESP32 (DevKit V1, etc):
```bash
make esp32-build     # generate config.h + compile (pio run -e esp32)
make esp32-flash     # build + flash over USB (espflash)
make esp32-upload    # build + upload firmware to server for OTA
make esp32-monitor   # serial monitor
```

ESP32-C3 (DevKitM-1, etc) — set the onboard LED pin first:
```bash
export ESP32_LED_PIN=8
make esp32c3-build   # pio run -e esp32c3
make esp32c3-flash   # build + flash over USB
make esp32c3-upload  # OTA upload
```

Flashing uses `espflash` (not PlatformIO's esptool) to avoid pyserial issues
under nix. After editing firmware, verify both targets still compile:
`make esp32-build` and `make esp32c3-build`.

## Conventions

- The firmware scans for BLE advertisement names `Thermopro`, `ThermoPro`, and
  `TP25` — a wide match so no per-device discovery step is needed.
- Run `make fmt` before committing Go changes.
- Don't commit `esp32/src/config.h` (secrets) or build artifacts (`bin/`,
  `esp32/.pio/`).
