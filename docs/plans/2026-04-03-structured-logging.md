# Structured Logging Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add configurable structured logging via `log/slog` to debug WebAuthn passkey failures through Cloudflare tunnels.

**Architecture:** Set `slog.SetDefault()` once in `main.go` after config loads. Use `slog.Debug/Info/Warn/Error` throughout. Migrate all existing `log.Printf` calls. Add a `log_level` config field (default: `info`). Use `slog.NewTextHandler` for human-readable output.

**Tech Stack:** Go stdlib `log/slog`, koanf config

---

### Task 1: Add `LogLevel` config field

**Files:**
- Modify: `internal/config/config.go:23-29` (Config struct), `internal/config/config.go:39-44` (defaults map)
- Test: `internal/config/config_test.go`

**Step 1: Write the failing tests**

Add to `internal/config/config_test.go` — in `TestDefaults`, add:

```go
if cfg.LogLevel != "info" {
    t.Errorf("expected default log_level 'info', got %s", cfg.LogLevel)
}
```

Add a new test:

```go
func TestLogLevelEnvOverride(t *testing.T) {
    t.Setenv("THERM_PRO_LOG_LEVEL", "debug")
    cfg, err := Load("")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if cfg.LogLevel != "debug" {
        t.Errorf("expected log_level 'debug', got %s", cfg.LogLevel)
    }
}
```

**Step 2: Run tests to verify they fail**

Run: `make test`
Expected: FAIL — `cfg.LogLevel` is empty string, not `"info"`

**Step 3: Implement the config change**

In `internal/config/config.go`, add `LogLevel` field to Config struct:

```go
type Config struct {
    Port           int         `koanf:"port"`
    AllowedCIDR    string      `koanf:"allowed_cidr"`
    TrustProxy     bool        `koanf:"trust_proxy"`
    Slack          SlackConfig `koanf:"slack"`
    DataDir        string      `koanf:"data_dir"`
    WebAuthnOrigin string      `koanf:"webauthn_origin"`
    LogLevel       string      `koanf:"log_level"`
}
```

Add default in `Load()`:

```go
k.Load(confmap.Provider(map[string]interface{}{
    "port":            8088,
    "allowed_cidr":   "192.168.1.0/24",
    "trust_proxy":    false,
    "webauthn_origin": "http://localhost:8088",
    "log_level":      "info",
}, "."), nil)
```

No env mapper change needed — `THERM_PRO_LOG_LEVEL` maps to `log_level` via the default `strings.ToLower` path.

**Step 4: Run tests to verify they pass**

Run: `make test`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat: add log_level config field with default 'info'"
```

---

### Task 2: Initialize slog and add request logging middleware

**Files:**
- Modify: `cmd/therm-pro-server/main.go`

**Step 1: Implement slog initialization**

Replace the entire `cmd/therm-pro-server/main.go` with:

```go
// cmd/therm-pro-server/main.go
package main

import (
    "bufio"
    "context"
    "fmt"
    "log"
    "log/slog"
    "net"
    "net/http"
    "os"
    "os/signal"
    "strconv"
    "strings"
    "syscall"
    "time"

    "github.com/stahnma/therm-pro/internal/api"
    "github.com/stahnma/therm-pro/internal/config"
    "github.com/stahnma/therm-pro/internal/consul"
)

// GitCommit is set at build time via -ldflags.
var GitCommit = "dev"

func parseLogLevel(s string) slog.Level {
    switch strings.ToLower(s) {
    case "debug":
        return slog.LevelDebug
    case "warn", "warning":
        return slog.LevelWarn
    case "error":
        return slog.LevelError
    default:
        return slog.LevelInfo
    }
}

// statusWriter captures the HTTP status code for logging.
type statusWriter struct {
    http.ResponseWriter
    status int
    written bool
}

func (w *statusWriter) WriteHeader(code int) {
    if !w.written {
        w.status = code
        w.written = true
    }
    w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
    if !w.written {
        w.status = 200
        w.written = true
    }
    return w.ResponseWriter.Write(b)
}

// Hijack implements http.Hijacker for websocket support.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
    if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
        return hj.Hijack()
    }
    return nil, nil, fmt.Errorf("underlying ResponseWriter does not support Hijack")
}

func requestLogger(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        sw := &statusWriter{ResponseWriter: w}
        next.ServeHTTP(sw, r)
        slog.Debug("http request",
            "method", r.Method,
            "path", r.URL.Path,
            "status", sw.status,
            "duration", time.Since(start),
            "remote_addr", r.RemoteAddr)
    })
}

func main() {
    cfg, err := config.Load("")
    if err != nil {
        log.Fatalf("failed to load config: %v", err)
    }

    // Initialize structured logging.
    level := parseLogLevel(cfg.LogLevel)
    logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
    slog.SetDefault(logger)

    slog.Info("starting server", "port", cfg.Port, "commit", GitCommit, "log_level", cfg.LogLevel)

    srv := api.NewServer(cfg, GitCommit)
    mux := srv.Routes()

    if err := consul.Register(cfg.Port); err != nil {
        slog.Warn("consul registration failed", "error", err)
    }

    httpSrv := &http.Server{Addr: ":" + strconv.Itoa(cfg.Port), Handler: requestLogger(mux)}

    go func() {
        slog.Info("listening", "addr", httpSrv.Addr)
        if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
            slog.Error("server error", "error", err)
            os.Exit(1)
        }
    }()

    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    slog.Info("shutting down")
    consul.Deregister()
    httpSrv.Shutdown(context.Background())
}
```

**Step 2: Run build to verify compilation**

Run: `make build`
Expected: builds successfully

**Step 3: Run tests**

Run: `make test`
Expected: PASS

**Step 4: Commit**

```bash
git add cmd/therm-pro-server/main.go
git commit -m "feat: initialize slog with configurable log level and request logging middleware"
```

---

### Task 3: Add logging to WebAuthn ceremonies

**Files:**
- Modify: `internal/auth/webauthn.go`
- Test: `internal/auth/webauthn_test.go` (existing tests should still pass)

**Step 1: Add `log` field and update constructor**

In `internal/auth/webauthn.go`, add `"log/slog"` to imports (replace `"log"`). Add a `log *slog.Logger` field to `WebAuthnHandler`:

```go
type WebAuthnHandler struct {
    wa            *webauthn.WebAuthn
    credStore     *CredentialStore
    sessionSecret []byte
    log           *slog.Logger

    mu               sync.Mutex
    pendingSession   *webauthn.SessionData
    pendingCreatedAt time.Time
}
```

In `NewWebAuthnHandler`, after creating the webauthn instance, set the logger:

```go
log.Printf(...)  // REMOVE all existing log.Printf calls
```

Replace the return block with:

```go
handler := &WebAuthnHandler{
    wa:            wa,
    credStore:     credStore,
    sessionSecret: secret,
    log:           slog.Default().With("component", "webauthn"),
}
handler.log.Info("webauthn configured", "rp_id", rpID, "rp_origin", rpOrigin)
return handler, nil
```

**Step 2: Add logging to LoginBegin**

```go
func (h *WebAuthnHandler) LoginBegin(w http.ResponseWriter, r *http.Request) {
    h.log.Debug("login begin", "remote_addr", r.RemoteAddr, "user_agent", r.UserAgent())
    user := h.user()
    if len(user.credentials) == 0 {
        h.log.Warn("login begin rejected: no credentials registered")
        jsonError(w, "no credentials registered", http.StatusBadRequest)
        return
    }
    h.log.Debug("login begin: found credentials", "count", len(user.credentials))

    assertion, session, err := h.wa.BeginLogin(user)
    if err != nil {
        h.log.Error("begin login failed", "error", err)
        jsonError(w, "login failed", http.StatusInternalServerError)
        return
    }

    h.mu.Lock()
    h.pendingSession = session
    h.pendingCreatedAt = time.Now()
    h.mu.Unlock()

    h.log.Debug("login begin: challenge issued")
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(assertion)
}
```

**Step 3: Add logging to LoginFinish**

```go
func (h *WebAuthnHandler) LoginFinish(w http.ResponseWriter, r *http.Request) {
    h.log.Debug("login finish", "remote_addr", r.RemoteAddr)
    user := h.user()

    h.mu.Lock()
    sessionData := h.pendingValid()
    h.mu.Unlock()

    if sessionData == nil {
        h.log.Warn("login finish rejected: no pending session (expired or missing)")
        jsonError(w, "no pending login", http.StatusBadRequest)
        return
    }

    _, err := h.wa.FinishLogin(user, *sessionData, r)
    if err != nil {
        h.log.Error("login verification failed", "error", err)
        jsonError(w, "login verification failed", http.StatusBadRequest)
        return
    }

    h.mu.Lock()
    h.pendingSession = nil
    h.mu.Unlock()

    SetSessionCookie(w, h.sessionSecret)
    h.log.Info("login succeeded", "remote_addr", r.RemoteAddr)

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

**Step 4: Add logging to RegisterBegin**

```go
func (h *WebAuthnHandler) RegisterBegin(w http.ResponseWriter, r *http.Request) {
    h.log.Debug("register begin", "remote_addr", r.RemoteAddr)
    user := h.user()

    creation, session, err := h.wa.BeginRegistration(user)
    if err != nil {
        h.log.Error("begin registration failed", "error", err)
        jsonError(w, "registration failed", http.StatusInternalServerError)
        return
    }

    h.mu.Lock()
    h.pendingSession = session
    h.pendingCreatedAt = time.Now()
    h.mu.Unlock()

    h.log.Debug("register begin: challenge issued")
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(creation)
}
```

**Step 5: Add logging to RegisterFinish**

```go
func (h *WebAuthnHandler) RegisterFinish(w http.ResponseWriter, r *http.Request) {
    h.log.Debug("register finish", "remote_addr", r.RemoteAddr)
    user := h.user()

    h.mu.Lock()
    sessionData := h.pendingValid()
    h.mu.Unlock()

    if sessionData == nil {
        h.log.Warn("register finish rejected: no pending registration")
        jsonError(w, "no pending registration", http.StatusBadRequest)
        return
    }

    credential, err := h.wa.FinishRegistration(user, *sessionData, r)
    if err != nil {
        h.log.Error("registration verification failed", "error", err)
        jsonError(w, "registration verification failed", http.StatusBadRequest)
        return
    }

    h.mu.Lock()
    h.pendingSession = nil
    h.mu.Unlock()

    h.credStore.Add(StoredCredential{
        ID:        credential.ID,
        PublicKey: credential.PublicKey,
        Label:     "Passkey",
    })
    if err := h.credStore.Save(); err != nil {
        h.log.Error("failed to save credential", "error", err)
        jsonError(w, "failed to save credential", http.StatusInternalServerError)
        return
    }

    h.log.Info("passkey registered", "remote_addr", r.RemoteAddr)
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

**Step 6: Run tests**

Run: `make test`
Expected: PASS — all existing webauthn tests should still pass since logging is side-effect only

**Step 7: Commit**

```bash
git add internal/auth/webauthn.go
git commit -m "feat: add structured logging to WebAuthn ceremonies"
```

---

### Task 4: Add logging to middleware, session validation, and auth status

**Files:**
- Modify: `internal/auth/middleware.go`
- Modify: `internal/auth/session.go`
- Modify: `internal/auth/status.go`

**Step 1: Add logging to `IsHomeNetwork` and middleware in `middleware.go`**

Add `"log/slog"` to imports. Replace function bodies:

```go
func IsHomeNetwork(r *http.Request, cidr string, trustProxy bool) bool {
    _, subnet, err := net.ParseCIDR(cidr)
    if err != nil {
        slog.Warn("invalid CIDR config", "cidr", cidr, "error", err)
        return false
    }

    ipStr := r.RemoteAddr
    ipSource := "RemoteAddr"
    if trustProxy {
        if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
            ipStr = strings.TrimSpace(strings.Split(xff, ",")[0])
            ipSource = "X-Forwarded-For"
        }
    }

    host, _, err := net.SplitHostPort(ipStr)
    if err != nil {
        host = ipStr
    }

    ip := net.ParseIP(host)
    if ip == nil {
        slog.Warn("failed to parse IP", "ip_str", ipStr, "source", ipSource)
        return false
    }

    result := subnet.Contains(ip)
    slog.Debug("home network check", "ip", host, "source", ipSource, "cidr", cidr, "result", result)
    return result
}
```

In `RequireHomeNetwork`, add logging on denial:

```go
func RequireHomeNetwork(cidr string, trustProxy bool) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if IsHomeNetwork(r, cidr, trustProxy) {
                next.ServeHTTP(w, r)
                return
            }
            slog.Info("access denied: not home network", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
            w.Header().Set("Content-Type", "application/json")
            w.WriteHeader(http.StatusForbidden)
            json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
        })
    }
}
```

In `RequireAuth`, add logging on denial:

```go
func RequireAuth(cidr string, trustProxy bool, validateSession SessionValidator) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if IsHomeNetwork(r, cidr, trustProxy) {
                next.ServeHTTP(w, r)
                return
            }
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

**Step 2: Add logging to session validation in `session.go`**

Add `"log/slog"` to imports. Update `ValidateSessionCookie`:

```go
func ValidateSessionCookie(r *http.Request, secret []byte) bool {
    cookie, err := r.Cookie(sessionCookieName)
    if err != nil {
        slog.Debug("session: no cookie")
        return false
    }

    parts := strings.SplitN(cookie.Value, ".", 2)
    if len(parts) != 2 {
        slog.Debug("session: malformed cookie value")
        return false
    }

    expiryStr, sig := parts[0], parts[1]

    expected := computeHMAC(expiryStr, secret)
    if !hmac.Equal([]byte(sig), []byte(expected)) {
        slog.Warn("session: HMAC mismatch")
        return false
    }

    expiry, err := strconv.ParseInt(expiryStr, 10, 64)
    if err != nil {
        slog.Debug("session: invalid expiry", "value", expiryStr)
        return false
    }

    if time.Now().Unix() >= expiry {
        slog.Debug("session: expired")
        return false
    }

    slog.Debug("session: valid")
    return true
}
```

**Step 3: Add logging to status handler in `status.go`**

Add `"log/slog"` to imports. Add a debug log line in the handler:

```go
func StatusHandler(cidr string, trustProxy bool, validateSession SessionValidator) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        isHome := IsHomeNetwork(r, cidr, trustProxy)
        isAuthed := validateSession != nil && validateSession(r)

        resp := StatusResponse{
            Role:        "viewer",
            CanRegister: isHome,
        }
        if isHome || isAuthed {
            resp.Role = "admin"
        }

        slog.Debug("auth status", "role", resp.Role, "is_home", isHome, "is_authed", isAuthed, "remote_addr", r.RemoteAddr)

        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(resp)
    }
}
```

**Step 4: Run tests**

Run: `make test`
Expected: PASS — all middleware, session, and status tests should still pass

**Step 5: Commit**

```bash
git add internal/auth/middleware.go internal/auth/session.go internal/auth/status.go
git commit -m "feat: add structured logging to auth middleware, session, and status"
```

---

### Task 5: Migrate remaining `log.Printf` calls to slog

**Files:**
- Modify: `internal/api/routes.go` — 2 calls (lines 24, 37)
- Modify: `internal/api/handlers.go` — 3 calls (lines 73, 130, 139)
- Modify: `internal/api/websocket.go` — 1 call (line 25)
- Modify: `internal/consul/consul.go` — 3 calls (lines 102, 159, 163)
- Modify: `internal/slack/command.go` — 3 calls (lines 42, 66, 74)

**Step 1: Migrate `internal/api/routes.go`**

Replace `"log"` import with `"log/slog"`. Change:
- Line 24: `log.Printf("WARNING: failed to load credential store: %v", err)` → `slog.Warn("failed to load credential store", "error", err)`
- Line 37: `log.Printf("WARNING: WebAuthn setup failed: %v", err)` → `slog.Warn("webauthn setup failed", "error", err)`

Add after line 39 (inside the `else` block after webauthnHandler is set):
```go
slog.Info("webauthn configured", "origin", s.config.WebAuthnOrigin)
```

**Step 2: Migrate `internal/api/handlers.go`**

Replace `"log"` import with `"log/slog"`. Change:
- Line 73: `log.Printf("warning: could not load session: %v", err)` → `slog.Warn("could not load session", "error", err)`
- Line 130: `log.Printf("warning: could not save session: %v", err)` → `slog.Warn("could not save session", "error", err)`
- Line 139: `log.Printf("ALERT: %s", alert.Message)` → `slog.Info("alert fired", "message", alert.Message)`

**Step 3: Migrate `internal/api/websocket.go`**

Replace `"log"` import with `"log/slog"`. Change:
- Line 25: `log.Printf("websocket upgrade: %v", err)` → `slog.Error("websocket upgrade failed", "error", err)`

**Step 4: Migrate `internal/consul/consul.go`**

Replace `"log"` import with `"log/slog"`. Change:
- Line 102: `log.Printf("consul: registered as %q at %s:%d", serviceID, ip, port)` → `slog.Info("consul registered", "service_id", serviceID, "ip", ip, "port", port)`
- Line 159: `log.Printf("consul: deregister failed: %v", err)` → `slog.Error("consul deregister failed", "error", err)`
- Line 163: `log.Printf("consul: deregistered %q", serviceID)` → `slog.Info("consul deregistered", "service_id", serviceID)`

**Step 5: Migrate `internal/slack/command.go`**

Replace `"log"` import with `"log/slog"`. Change:
- Line 42: `log.Printf("slack command: verification failed: %v", err)` → `slog.Warn("slack command verification failed", "error", err)`
- Line 66: `log.Printf("slack command: chart render failed: %v", err)` → `slog.Error("slack chart render failed", "error", err)`
- Line 74: `log.Printf("slack command: file upload failed: %v", err)` → `slog.Error("slack file upload failed", "error", err)`

**Step 6: Run tests and build**

Run: `make test && make build`
Expected: PASS and successful build

**Step 7: Commit**

```bash
git add internal/api/routes.go internal/api/handlers.go internal/api/websocket.go internal/consul/consul.go internal/slack/command.go
git commit -m "refactor: migrate all remaining log.Printf calls to slog"
```

---

### Task 6: Improve JavaScript error messages

**Files:**
- Modify: `internal/web/static/app.js:391-475`

**Step 1: Update `signIn()` error handling**

Change line 394 from:
```javascript
if (!beginResp.ok) { alert('Sign in not available'); return; }
```
to:
```javascript
if (!beginResp.ok) {
    const errData = await beginResp.json().catch(() => ({}));
    alert('Sign in not available: ' + (errData.error || beginResp.statusText));
    return;
}
```

Change lines 426-428 from:
```javascript
} else {
    alert('Sign in failed');
}
```
to:
```javascript
} else {
    const errData = await finishResp.json().catch(() => ({}));
    alert('Sign in failed: ' + (errData.error || finishResp.statusText));
}
```

**Step 2: Update `registerPasskey()` error handling**

Change line 438 from:
```javascript
if (!beginResp.ok) { alert('Registration not available'); return; }
```
to:
```javascript
if (!beginResp.ok) {
    const errData = await beginResp.json().catch(() => ({}));
    alert('Registration not available: ' + (errData.error || beginResp.statusText));
    return;
}
```

Change lines 466-469 from:
```javascript
if (finishResp.ok) {
    alert('Passkey registered!');
} else {
    alert('Registration failed');
}
```
to:
```javascript
if (finishResp.ok) {
    alert('Passkey registered!');
} else {
    const errData = await finishResp.json().catch(() => ({}));
    alert('Registration failed: ' + (errData.error || finishResp.statusText));
}
```

**Step 3: Run build**

Run: `make build`
Expected: builds successfully (JS is embedded at build time)

**Step 4: Commit**

```bash
git add internal/web/static/app.js
git commit -m "fix: show server error details in auth failure alerts"
```

---

## Verification

After all tasks are complete:

1. `make build` succeeds
2. `make test` passes
3. Manual test: `THERM_PRO_LOG_LEVEL=debug ./bin/therm-pro-server` — verify structured debug output on stderr
4. Manual test: `THERM_PRO_LOG_LEVEL=info ./bin/therm-pro-server` — verify debug lines are suppressed, info lines appear
5. Verify no `"log"` imports remain (except `log.Fatalf` in main.go before slog init):
   ```bash
   grep -r '"log"' internal/ cmd/
   ```
   Expected: only `cmd/therm-pro-server/main.go` (for the pre-slog `log.Fatalf`)
