# Registration PIN + Remove Home Network Detection

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove IP-based home network detection entirely. Access is now binary: authenticated (passkey session) = admin, unauthenticated = read-only viewer. Passkey registration is gated by a configurable PIN instead of a network check.

**Architecture:** Remove `allowed_cidr`, `trust_proxy`, `IsHomeNetwork`, `RequireHomeNetwork` and all references. Replace `RequireAuth` with a simpler session-only check. Add `registration_pin` config field. Registration endpoints validate PIN from request header. Status endpoint returns `role` based on session only, and `can_register` based on whether a PIN is configured.

**Tech Stack:** Go 1.25, existing config/auth packages, vanilla JS

**Context:** The original design gated registration and admin access on home network IP matching (`allowed_cidr`). This doesn't work through Cloudflare Tunnel — the forwarded IP is the client's public IP, not a LAN IP. Since all access goes through the tunnel, the home network concept is removed entirely. See `docs/plans/2026-04-03-access-control-design.md` for the original (now superseded) design.

**New access model:**

| Condition | Role | Can register? |
|---|---|---|
| Valid passkey session cookie | admin | n/a (already registered) |
| No session, PIN configured | viewer | Yes (must enter PIN) |
| No session, no PIN configured | viewer | No |

---

### Task 1: Add `registration_pin` to config, remove `allowed_cidr` and `trust_proxy`

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Step 1: Write the failing tests**

Add to `config_test.go`:

```go
func TestRegistrationPinDefault(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RegistrationPIN != "" {
		t.Errorf("expected empty default registration_pin, got %q", cfg.RegistrationPIN)
	}
}

func TestRegistrationPinEnvOverride(t *testing.T) {
	t.Setenv("THERM_PRO_REGISTRATION_PIN", "5678")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RegistrationPIN != "5678" {
		t.Errorf("expected registration_pin 5678, got %q", cfg.RegistrationPIN)
	}
}
```

Remove all tests that reference `AllowedCIDR` or `TrustProxy` — update `TestDefaults`, `TestEnvOverride`, `TestYAMLConfig`, `TestDotEnvConfig` accordingly (remove the CIDR/trust_proxy assertions and env vars from those tests).

**Step 2: Run test to verify it fails**

Run: `make test`
Expected: FAIL — `cfg.RegistrationPIN` undefined

**Step 3: Write minimal implementation**

In `config.go`, update the `Config` struct — remove `AllowedCIDR` and `TrustProxy`, add `RegistrationPIN`:

```go
type Config struct {
	Port            int         `koanf:"port"`
	Slack           SlackConfig `koanf:"slack"`
	DataDir         string      `koanf:"data_dir"`
	WebAuthnOrigin  string      `koanf:"webauthn_origin"`
	LogLevel        string      `koanf:"log_level"`
	RegistrationPIN string      `koanf:"registration_pin"`
}
```

Remove `allowed_cidr` and `trust_proxy` from the defaults map in `Load()`.

**Step 4: Run test to verify it passes**

Run: `make test`
Expected: Compilation errors in other packages that still reference `AllowedCIDR`/`TrustProxy` — that's OK, we fix those in subsequent tasks.

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: replace allowed_cidr/trust_proxy with registration_pin in config"
```

---

### Task 2: Simplify auth middleware — remove home network checks

**Files:**
- Modify: `internal/auth/middleware.go`
- Modify: `internal/auth/middleware_test.go`

**Step 1: Rewrite middleware.go**

Remove `IsHomeNetwork` and `RequireHomeNetwork`. Simplify `RequireAuth` to session-only:

```go
package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// SessionValidator checks whether the current request carries a valid session.
type SessionValidator func(r *http.Request) bool

// RequireAuth returns middleware that blocks requests not carrying a valid session.
func RequireAuth(validateSession SessionValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if validateSession != nil && validateSession(r) {
				next.ServeHTTP(w, r)
				return
			}
			slog.Info("access denied: unauthorized", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		})
	}
}
```

**Step 2: Rewrite middleware_test.go**

Remove all `IsHomeNetwork` and `RequireHomeNetwork` tests. Update `RequireAuth` tests to only test session validation:

```go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAuth_WithValidSession(t *testing.T) {
	validator := func(r *http.Request) bool { return true }
	handler := RequireAuth(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/session/reset", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRequireAuth_WithoutSession(t *testing.T) {
	validator := func(r *http.Request) bool { return false }
	handler := RequireAuth(validator)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/session/reset", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestRequireAuth_NilValidator(t *testing.T) {
	handler := RequireAuth(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/session/reset", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
```

**Step 3: Run tests**

Run: `make test`
Expected: Compilation errors in `routes.go`, `status.go`, `handlers_test.go` — fixed in subsequent tasks.

**Step 4: Commit**

```bash
git add internal/auth/middleware.go internal/auth/middleware_test.go
git commit -m "refactor: remove home network detection, simplify RequireAuth to session-only"
```

---

### Task 3: Simplify status endpoint

**Files:**
- Modify: `internal/auth/status.go`

**Step 1: Update StatusHandler**

Remove CIDR/proxy params. Role is based on session only. `can_register` is based on PIN config:

```go
package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type StatusResponse struct {
	Role        string `json:"role"`
	CanRegister bool   `json:"can_register"`
}

// StatusHandler returns an http.HandlerFunc that reports the caller's access level.
func StatusHandler(validateSession SessionValidator, registrationPIN string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		isAuthed := validateSession != nil && validateSession(r)

		resp := StatusResponse{
			Role:        "viewer",
			CanRegister: registrationPIN != "",
		}
		if isAuthed {
			resp.Role = "admin"
		}

		slog.Debug("auth status", "role", resp.Role, "can_register", resp.CanRegister, "is_authed", isAuthed, "remote_addr", r.RemoteAddr)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
```

**Step 2: Run tests**

Run: `make test`
Expected: Compilation errors in `routes.go` — fixed in next task.

**Step 3: Commit**

```bash
git add internal/auth/status.go
git commit -m "refactor: simplify status endpoint — session-based role, PIN-based can_register"
```

---

### Task 4: Add PIN validation to WebAuthn registration

**Files:**
- Modify: `internal/auth/webauthn.go`
- Create: `internal/auth/webauthn_test.go`

**Step 1: Write the failing test**

Create `internal/auth/webauthn_test.go`:

```go
package auth

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterBeginRequiresPIN(t *testing.T) {
	h := &WebAuthnHandler{
		registrationPIN: "1234",
		log:             slog.Default().With("component", "webauthn-test"),
	}

	req := httptest.NewRequest("POST", "/auth/register/begin", nil)
	rec := httptest.NewRecorder()
	h.RegisterBegin(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 without PIN, got %d", rec.Code)
	}
}

func TestRegisterBeginWrongPIN(t *testing.T) {
	h := &WebAuthnHandler{
		registrationPIN: "1234",
		log:             slog.Default().With("component", "webauthn-test"),
	}

	req := httptest.NewRequest("POST", "/auth/register/begin", nil)
	req.Header.Set("X-Registration-PIN", "9999")
	rec := httptest.NewRecorder()
	h.RegisterBegin(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 with wrong PIN, got %d", rec.Code)
	}
}

func TestRegisterBeginNoPINConfigured(t *testing.T) {
	h := &WebAuthnHandler{
		registrationPIN: "",
		log:             slog.Default().With("component", "webauthn-test"),
	}

	req := httptest.NewRequest("POST", "/auth/register/begin", nil)
	rec := httptest.NewRecorder()
	h.RegisterBegin(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 when no PIN configured, got %d", rec.Code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `make test`
Expected: FAIL — `registrationPIN` field does not exist

**Step 3: Write implementation**

In `webauthn.go`:

Add `registrationPIN string` field to `WebAuthnHandler` struct:

```go
type WebAuthnHandler struct {
	wa              *webauthn.WebAuthn
	credStore       *CredentialStore
	sessionSecret   []byte
	registrationPIN string
	log             *slog.Logger

	mu               sync.Mutex
	pendingSession   *webauthn.SessionData
	pendingCreatedAt time.Time
}
```

Update `NewWebAuthnHandler` signature to accept PIN:

```go
func NewWebAuthnHandler(rpName, rpOrigin, registrationPIN string, credStore *CredentialStore, dataDir string) (*WebAuthnHandler, error) {
```

Set it in the constructor:

```go
handler := &WebAuthnHandler{
	wa:              wa,
	credStore:       credStore,
	sessionSecret:   secret,
	registrationPIN: registrationPIN,
	log:             slog.Default().With("component", "webauthn"),
}
```

Add PIN validation helper:

```go
// checkRegistrationPIN validates the PIN from the X-Registration-PIN header.
// Returns true if the PIN is valid, false (and writes error response) otherwise.
func (h *WebAuthnHandler) checkRegistrationPIN(w http.ResponseWriter, r *http.Request) bool {
	if h.registrationPIN == "" {
		h.log.Warn("registration rejected: no registration PIN configured")
		jsonError(w, "registration not available", http.StatusForbidden)
		return false
	}
	pin := r.Header.Get("X-Registration-PIN")
	if pin != h.registrationPIN {
		h.log.Warn("registration rejected: invalid PIN", "remote_addr", r.RemoteAddr)
		jsonError(w, "invalid registration PIN", http.StatusForbidden)
		return false
	}
	return true
}
```

Add `if !h.checkRegistrationPIN(w, r) { return }` at the top of both `RegisterBegin` and `RegisterFinish`.

**Step 4: Run test to verify it passes**

Run: `make test`
Expected: PASS for webauthn tests; routes.go may still have compilation errors.

**Step 5: Commit**

```bash
git add internal/auth/webauthn.go internal/auth/webauthn_test.go
git commit -m "feat: gate passkey registration behind PIN instead of IP check"
```

---

### Task 5: Update route wiring and handler tests

**Files:**
- Modify: `internal/api/routes.go`
- Modify: `internal/api/handlers_test.go`

**Step 1: Update routes.go**

Remove `requireHome`. Update `requireAuth` and `StatusHandler` calls. Pass PIN to `NewWebAuthnHandler`. Remove registration routes from `requireHome`:

```go
requireAuth := auth.RequireAuth(sessionValidator)
// Remove: requireHome := auth.RequireHomeNetwork(...)

// ...

mux.HandleFunc("GET /api/auth/status", auth.StatusHandler(sessionValidator, s.config.RegistrationPIN))

// ...

// WebAuthn passkey routes
if webauthnHandler != nil {
	mux.HandleFunc("POST /auth/register/begin", webauthnHandler.RegisterBegin)
	mux.HandleFunc("POST /auth/register/finish", webauthnHandler.RegisterFinish)
	mux.HandleFunc("POST /auth/login/begin", webauthnHandler.LoginBegin)
	mux.HandleFunc("POST /auth/login/finish", webauthnHandler.LoginFinish)
}
```

Update `NewWebAuthnHandler` call:

```go
wh, err := auth.NewWebAuthnHandler(
	"Therm-Pro",
	s.config.WebAuthnOrigin,
	s.config.RegistrationPIN,
	credStore,
	s.config.DataDir,
)
```

**Step 2: Update handlers_test.go**

Remove `AllowedCIDR` from all `config.Config` literals in test helpers. The config no longer has that field.

**Step 3: Run tests**

Run: `make test`
Expected: PASS — all compilation errors resolved.

**Step 4: Commit**

```bash
git add internal/api/routes.go internal/api/handlers_test.go
git commit -m "refactor: wire up session-only auth and PIN registration in routes"
```

---

### Task 6: Update UI — prompt for PIN before registration

**Files:**
- Modify: `internal/web/static/app.js`

**Step 1: Update registerPasskey() to prompt for PIN**

Replace the `registerPasskey` function to prompt for PIN and send it as a header:

```javascript
async function registerPasskey() {
  const pin = prompt('Enter registration PIN:');
  if (!pin) return;

  try {
    const beginResp = await fetch('/auth/register/begin', {
      method: 'POST',
      headers: { 'X-Registration-PIN': pin },
    });
    if (!beginResp.ok) {
      const errData = await beginResp.json().catch(() => ({}));
      alert('Registration not available: ' + (errData.error || beginResp.statusText));
      return;
    }
    const options = await beginResp.json();

    options.publicKey.challenge = base64urlToBuffer(options.publicKey.challenge);
    options.publicKey.user.id = base64urlToBuffer(options.publicKey.user.id);
    if (options.publicKey.excludeCredentials) {
      options.publicKey.excludeCredentials = options.publicKey.excludeCredentials.map(c => ({
        ...c,
        id: base64urlToBuffer(c.id),
      }));
    }

    const credential = await navigator.credentials.create({ publicKey: options.publicKey });

    const finishResp = await fetch('/auth/register/finish', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-Registration-PIN': pin,
      },
      body: JSON.stringify({
        id: credential.id,
        rawId: bufferToBase64url(credential.rawId),
        type: credential.type,
        response: {
          attestationObject: bufferToBase64url(credential.response.attestationObject),
          clientDataJSON: bufferToBase64url(credential.response.clientDataJSON),
        },
      }),
    });

    if (finishResp.ok) {
      alert('Passkey registered!');
      location.reload();
    } else {
      const errData = await finishResp.json().catch(() => ({}));
      alert('Registration failed: ' + (errData.error || finishResp.statusText));
    }
  } catch (err) {
    console.error('Register error:', err);
    alert('Registration failed: ' + err.message);
  }
}
```

**Step 2: Manual test**

1. Set `registration_pin: "1234"` in `~/.therm-pro/config.yaml`
2. Start the server: `make run`
3. Open dashboard — "Register Passkey" button should be visible
4. Click it — should prompt for PIN
5. Enter wrong PIN — should show "invalid registration PIN" error
6. Enter correct PIN — should trigger WebAuthn browser prompt

**Step 3: Commit**

```bash
git add internal/web/static/app.js
git commit -m "feat: prompt for registration PIN in UI before passkey registration"
```

---

### Task 7: Update README

**Files:**
- Modify: `README.md`

**Step 1: Update config table**

Remove `allowed_cidr` and `trust_proxy` rows. Add `registration_pin` row.

**Step 2: Update access control / passkey sections**

- Remove references to "home network" detection
- Update the access model description: authenticated = admin, unauthenticated = read-only
- Update passkey registration instructions: set PIN in config, enter in browser
- Remove Tailscale CIDR note if present

**Step 3: Commit**

```bash
git add README.md
git commit -m "docs: update README — remove home network, document registration PIN"
```

---

### Task 8: Update design doc with revision

**Files:**
- Modify: `docs/plans/2026-04-03-access-control-design.md`

**Step 1: Add revision section at the top**

Add a dated revision note explaining the change: home network IP detection removed because it doesn't work through Cloudflare Tunnel (forwarded IP is the client's public IP, not LAN IP). Access model simplified to authenticated/unauthenticated. Registration gated by PIN.

**Step 2: Commit**

```bash
git add docs/plans/2026-04-03-access-control-design.md
git commit -m "docs: revise access control design — remove home network, add registration PIN"
```
