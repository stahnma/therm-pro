# Slack Slash Command Integration

## Overview

Add a `/tp25` Slack slash command that queries the ThermoPro TP25 server and
returns current probe temperatures, battery level, and a session temperature
graph as a PNG image — all visible in-channel.

## Architecture

The integration is built into the existing Go server. No new services to deploy.

### Request Flow

```
User types /tp25 in Slack
  → Slack POSTs to https://<your-domain>/slack/command
  → Server verifies request via Slack signing secret (HMAC-SHA256)
  → Server reads current session data (probes, history, battery)
  → Server generates PNG chart from session history using go-chart
  → Server uploads PNG to Slack via files.uploadV2 API (Bot Token)
  → Server responds in_channel with formatted text + chart
```

### Response Format

```
BBQ Status (session started 3h 22m ago)
  Pit (Probe 1):  275.3°F
  Brisket (Probe 2):  187.1°F
  Probe 3:  disconnected
  Probe 4:  disconnected
  Battery: 87%
```

Plus an attached PNG chart of the session's temperature history.

## Chart Generation

Uses `github.com/wcharczuk/go-chart/v2` — pure Go, no CGo, no external
dependencies.

- One line per connected probe, color-coded to match web UI probe colors
- X-axis: time since session start (e.g., "0h", "1h", "2h")
- Y-axis: temperature in °F
- Legend with probe labels
- Horizontal dashed lines for configured target temperatures
- Dark background to match web UI default theme
- 800x400px PNG, rendered in memory (no temp files)

### Function Signature

```go
func RenderSessionChart(history []cook.Reading, probes [4]cook.Probe) ([]byte, error)
```

## Security

- All incoming requests verified via Slack signing secret (HMAC-SHA256 of
  request body + timestamp header)
- Bot Token used only for outbound API calls (file upload), never exposed
- No new listening ports — adds one route to the existing HTTP server
- Only new Go dependency is `go-chart` (pure Go, well-maintained)

## Slack App Setup

1. Create app at api.slack.com/apps
2. Add Slash Command: `/tp25` → `https://<your-domain>/slack/command`
3. Add Bot Token Scopes: `commands`, `chat:write`, `files:write`
4. Install to workspace
5. Grab Bot Token and Signing Secret

## Network Access (Cloudflare Tunnel)

The slash command requires Slack to reach the server over HTTPS. Since the
server runs on a home network, use Cloudflare Tunnel (`cloudflared`):

- Outbound-only connection from home to Cloudflare — no port forwarding
- Stable URL (e.g., `bbq.yourdomain.com`) with automatic HTTPS
- Free tier is sufficient
- Runs as a systemd service alongside therm-pro-server
- Can be scoped to only expose `/slack/command`

## Configuration

New environment variables (extending existing config pattern):

- `SLACK_SIGNING_SECRET` — for request verification
- `SLACK_BOT_TOKEN` — for file uploads

Existing `SLACK_WEBHOOK_URL` remains unchanged for push alerts.

## File Changes

### New Files

- `internal/slack/command.go` — slash command handler (request verification,
  response formatting, file upload to Slack)
- `internal/chart/chart.go` — PNG chart renderer
- `internal/chart/chart_test.go` — chart generation test with synthetic data

### Modified Files

- `internal/api/handlers.go` — add `/slack/command` route, pass signing secret
  and bot token to Slack client
- `internal/slack/slack.go` — extend `Client` with bot token and signing secret
- `cmd/therm-pro-server/main.go` — accept `SLACK_SIGNING_SECRET` and
  `SLACK_BOT_TOKEN` env vars/flags
- `go.mod` — add `github.com/wcharczuk/go-chart/v2`

### Unchanged

- ESP32 firmware — no changes needed
- Existing webhook alerts — keep working as-is
- Session persistence — no schema changes
- Web UI — untouched

## Testing

- Unit test for chart generation with synthetic session data
- Manual testing with ngrok during development before cloudflared setup
- Verify signing secret validation rejects forged requests
