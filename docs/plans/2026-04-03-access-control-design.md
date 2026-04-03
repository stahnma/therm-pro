# Access Control: Read-Only Public View with Passkey Auth

**Issue:** #5
**Date:** 2026-04-03

## Goal

Public users get a read-only dashboard. Write operations (reset cook, set alerts/labels) require either being on the home network or authenticating with a passkey.

## Access Control Model

Three tiers, evaluated in order:

1. **Home network (CIDR match)** — full read/write, no auth required. Default `192.168.1.0/24`, configurable. Checks source IP directly, or `X-Forwarded-For` when `trust_proxy: true`.
2. **Authenticated via passkey** — full read/write from anywhere. WebAuthn passkey (1Password, Face ID, etc.). Session stored in a secure cookie.
3. **Everyone else** — read-only.

### Protected endpoints

- `POST /api/session/reset` — reset cook
- `POST /api/alerts` — set labels/alert thresholds
- `POST /api/firmware/upload` — upload firmware
- `POST /auth/register/begin` and `/auth/register/finish` — passkey registration (home network only)

### Public endpoints (unchanged)

- `GET /` — dashboard
- `GET /api/session` — current session data
- `WS /api/ws` — live updates
- `GET /diagnostics` — system health
- `POST /auth/login/begin` and `/auth/login/finish` — passkey authentication

## Configuration with Koanf

Introduce an `internal/config` package using Koanf. Configuration layers (each overrides the previous):

1. Hardcoded defaults
2. Config file: `~/.therm-pro/config.yaml`
3. `.env` file: `~/.therm-pro/.env`
4. Environment variables (prefixed `THERM_PRO_`)

Example `config.yaml`:

```yaml
port: 8088
allowed_cidr: "192.168.1.0/24"
trust_proxy: false
webauthn_rp_id: "localhost"        # set to your domain when behind a reverse proxy
webauthn_origin: "http://localhost:8088"  # set to your public URL for passkey auth

slack:
  webhook: ""
  signing_secret: ""
  bot_token: ""
```

All existing env vars (`PORT`, `THERM_PRO_SLACK_WEBHOOK`, etc.) continue to work. The config file is optional — the app works with zero config using defaults + env vars.

### Tailscale note

Users running Tailscale can set `allowed_cidr: "100.64.0.0/10"` to trust Tailscale IPs directly, with no proxy trust needed.

## WebAuthn Passkey Flow

**Library:** `go-webauthn/webauthn`

### Registration (home network only)

1. User clicks "Register Passkey" in the dashboard (only visible on home network)
2. Server generates a registration challenge, browser passes it to authenticator
3. User approves with biometrics, browser sends credential back
4. Server stores the public key + credential ID in `~/.therm-pro/passkeys.json`

### Authentication (from anywhere)

1. User clicks "Sign In" on the dashboard
2. Server generates an authentication challenge
3. Authenticator signs it, browser sends response
4. Server validates signature, sets a session cookie (HTTP-only, secure, `SameSite=Lax`)
5. Subsequent requests authenticated via cookie

**Session lifetime:** 24 hours. Sign in once at the start of a cook.

### Auth endpoints

- `POST /auth/register/begin` — start registration (home network only)
- `POST /auth/register/finish` — complete registration (home network only)
- `POST /auth/login/begin` — start authentication
- `POST /auth/login/finish` — complete authentication

## Dashboard UI Changes

### Read-only visitor sees

- Probe cards with temperatures (no tap-to-edit)
- Temperature chart
- Battery level
- A small "Sign In" link in the nav bar

### Home network / authenticated user sees

- Everything above, plus:
- Tap-to-edit on probe cards (labels, alert thresholds)
- "Reset Cook" button
- "Register Passkey" link (home network only)

### Auth status endpoint

`GET /api/auth/status` returns the user's access level:

```json
{ "role": "admin" }
{ "role": "viewer" }
```

Dashboard JS calls this on load and conditionally renders edit controls. The server enforces access on write endpoints regardless — UI hiding is UX, not security.

### Sign-in UX

Clicking "Sign In" triggers the WebAuthn browser prompt (1Password pops up). On success, page reloads with full controls. No separate login page.

## Middleware & Server Architecture

### New packages

- `internal/config` — Koanf-based config loading
- `internal/auth` — middleware, passkey storage, WebAuthn handlers

### Middleware chain on protected routes

```
Request → checkHomeNetwork → checkSession → deny (401)
```

If either check passes, the request proceeds. If both fail, return 401.

**checkHomeNetwork:**
- Direct connection: compare `request.RemoteAddr` against configured CIDR
- Behind proxy (`trust_proxy: true`): parse `X-Forwarded-For` and compare

**checkSession:**
- Validate the session cookie, check it's not expired

### Refactoring

`NewServer` currently takes 7 individual parameters. Refactor to accept a single `config.Config` struct.

### Migration path

Zero breaking changes. Without a config file or new env vars, the server behaves exactly as today — except write endpoints are restricted to `192.168.1.0/24` by default.
