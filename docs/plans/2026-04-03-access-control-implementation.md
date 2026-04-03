# Access Control Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add read-only public access with home network CIDR check and WebAuthn passkey auth for remote write access.

**Architecture:** New `internal/config` package (Koanf) replaces raw `os.Getenv` calls. New `internal/auth` package provides middleware that gates write endpoints behind network check or session cookie. WebAuthn passkey registration (home-network-only) and login endpoints enable remote auth. Dashboard JS conditionally renders edit controls based on `/api/auth/status`.

**Tech Stack:** Go 1.25, Koanf (config), go-webauthn/webauthn (passkeys), stdlib net/http middleware

---

### Task 1: Add Koanf and WebAuthn dependencies

**Files:**
- Modify: `go.mod`

**Step 1: Add dependencies**

Run:
```bash
cd /home/stahnma/development/personal/therm-pro && go get github.com/koanf/koanf/v2 github.com/koanf/providers/file github.com/koanf/providers/env github.com/koanf/parsers/yaml github.com/koanf/providers/confmap github.com/joho/godotenv github.com/koanf/providers/rawbytes github.com/go-webauthn/webauthn
```

**Step 2: Tidy**

Run: `make test`
Expected: existing tests still pass, `go.sum` updated.

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add koanf and go-webauthn dependencies"
```

---

### Task 2: Create config package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

**Step 1: Write the failing test**

```go
// internal/config/config_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 8088 {
		t.Errorf("expected port 8088, got %d", cfg.Port)
	}
	if cfg.AllowedCIDR != "192.168.1.0/24" {
		t.Errorf("expected default CIDR, got %s", cfg.AllowedCIDR)
	}
	if cfg.TrustProxy {
		t.Error("expected trust_proxy false by default")
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("THERM_PRO_PORT", "9090")
	t.Setenv("THERM_PRO_ALLOWED_CIDR", "10.0.0.0/8")
	t.Setenv("THERM_PRO_TRUST_PROXY", "true")
	t.Setenv("THERM_PRO_SLACK_WEBHOOK", "https://hooks.example.com/test")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.AllowedCIDR != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", cfg.AllowedCIDR)
	}
	if !cfg.TrustProxy {
		t.Error("expected trust_proxy true")
	}
	if cfg.Slack.Webhook != "https://hooks.example.com/test" {
		t.Errorf("expected slack webhook override, got %s", cfg.Slack.Webhook)
	}
}

func TestYAMLFile(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(yamlPath, []byte("port: 7070\nallowed_cidr: \"172.16.0.0/12\"\n"), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 7070 {
		t.Errorf("expected port 7070, got %d", cfg.Port)
	}
	if cfg.AllowedCIDR != "172.16.0.0/12" {
		t.Errorf("expected 172.16.0.0/12, got %s", cfg.AllowedCIDR)
	}
}

func TestEnvOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(yamlPath, []byte("port: 7070\n"), 0644)
	t.Setenv("THERM_PRO_PORT", "9999")

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 9999 {
		t.Errorf("expected env override 9999, got %d", cfg.Port)
	}
}

func TestLegacyPortEnv(t *testing.T) {
	t.Setenv("PORT", "3000")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Port != 3000 {
		t.Errorf("expected PORT=3000 override, got %d", cfg.Port)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `make test`
Expected: FAIL — `config` package doesn't exist yet.

**Step 3: Write the implementation**

```go
// internal/config/config.go
package config

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/koanf/koanf/v2"
	"github.com/koanf/parsers/yaml"
	"github.com/koanf/providers/confmap"
	"github.com/koanf/providers/env"
	"github.com/koanf/providers/file"
)

type SlackConfig struct {
	Webhook       string `koanf:"webhook"`
	SigningSecret string `koanf:"signing_secret"`
	BotToken      string `koanf:"bot_token"`
}

type Config struct {
	Port        int         `koanf:"port"`
	AllowedCIDR string      `koanf:"allowed_cidr"`
	TrustProxy  bool        `koanf:"trust_proxy"`
	Slack       SlackConfig `koanf:"slack"`
	DataDir     string      `koanf:"data_dir"`
}

// Load reads config from defaults, then config.yaml in dataDir (if present),
// then .env in dataDir (if present), then environment variables.
// Pass empty dataDir to use ~/.therm-pro.
func Load(dataDir string) (*Config, error) {
	k := koanf.New(".")

	// 1. Defaults
	k.Load(confmap.Provider(map[string]interface{}{
		"port":         8088,
		"allowed_cidr": "192.168.1.0/24",
		"trust_proxy":  false,
	}, "."), nil)

	// Resolve data dir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".therm-pro")
	}

	// 2. YAML config file (optional)
	yamlPath := filepath.Join(dataDir, "config.yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		k.Load(file.Provider(yamlPath), yaml.Parser())
	}

	// 3. .env file (optional) — parse as YAML-compat key=value
	envPath := filepath.Join(dataDir, ".env")
	if _, err := os.Stat(envPath); err == nil {
		loadDotEnv(k, envPath)
	}

	// 4. Environment variables (THERM_PRO_ prefix)
	k.Load(env.Provider("THERM_PRO_", ".", func(s string) string {
		// THERM_PRO_SLACK_WEBHOOK -> slack.webhook
		// THERM_PRO_PORT -> port
		// THERM_PRO_ALLOWED_CIDR -> allowed_cidr
		key := s[len("THERM_PRO_"):]
		switch key {
		case "SLACK_WEBHOOK":
			return "slack.webhook"
		case "SLACK_SIGNING_SECRET":
			return "slack.signing_secret"
		case "SLACK_BOT_TOKEN":
			return "slack.bot_token"
		default:
			return strings.ToLower(key)
		}
	}), nil)

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, err
	}
	cfg.DataDir = dataDir

	// 5. Legacy PORT env var (backwards compat)
	if p := os.Getenv("PORT"); p != "" {
		if pn, err := strconv.Atoi(p); err == nil {
			cfg.Port = pn
		}
	}

	return &cfg, nil
}
```

Note: `loadDotEnv` is a helper that reads the `.env` file using `godotenv` and loads key-value pairs into Koanf. The implementation should parse lines like `THERM_PRO_SLACK_WEBHOOK=value` and map them using the same key transform as the env provider. For the initial implementation, `.env` support can be deferred to a follow-up if it adds complexity — the YAML file and env vars cover the primary use cases.

**Step 4: Run tests to verify they pass**

Run: `make test`
Expected: All config tests PASS, existing tests still PASS.

**Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: add config package with Koanf-based loading"
```

---

### Task 3: Refactor NewServer to accept Config

**Files:**
- Modify: `internal/api/handlers.go:26-82` (Server struct and NewServer)
- Modify: `internal/api/routes.go:47-49` (Slack conditional)
- Modify: `internal/api/handlers_test.go:13-14,27,46,57` (all NewServer calls)
- Modify: `internal/api/websocket_test.go` (NewServer calls)
- Modify: `cmd/therm-pro-server/main.go` (use config.Load)

**Step 1: Update Server struct and NewServer**

In `internal/api/handlers.go`, change `NewServer` to accept `*config.Config`:

```go
import "github.com/stahnma/therm-pro/internal/config"

func NewServer(cfg *config.Config, gitCommit string) *Server {
	sessionPath := filepath.Join(cfg.DataDir, "session.json")
	firmwareDir := filepath.Join(cfg.DataDir, "firmware")

	session, err := cook.Load(sessionPath)
	if err != nil {
		log.Printf("warning: could not load session: %v", err)
		session = cook.NewSession()
	}
	return &Server{
		addr:               ":" + strconv.Itoa(cfg.Port),
		session:            session,
		alerts:             cook.NewAlertEngine(),
		slack:              slack.NewClient(cfg.Slack.Webhook),
		slackSigningSecret: cfg.Slack.SigningSecret,
		slackBotToken:      cfg.Slack.BotToken,
		firmware:           firmware.NewStore(firmwareDir),
		sessionPath:        sessionPath,
		gitCommit:          gitCommit,
		wsClients:          make(map[*wsClient]bool),
		config:             cfg,
	}
}
```

Add `config *config.Config` to the Server struct.

**Step 2: Update all test files**

Replace all `NewServer(":8088", "", "", "", "", "", "")` calls with:

```go
func testConfig() *config.Config {
	return &config.Config{
		Port:        8088,
		AllowedCIDR: "192.168.1.0/24",
		DataDir:     t.TempDir(),
	}
}

// In each test:
srv := NewServer(testConfig(), "test")
```

**Step 3: Update main.go**

```go
package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/stahnma/therm-pro/internal/api"
	"github.com/stahnma/therm-pro/internal/config"
	"github.com/stahnma/therm-pro/internal/consul"
)

var GitCommit = "dev"

func main() {
	cfg, err := config.Load("")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	srv := api.NewServer(cfg, GitCommit)
	mux := srv.Routes()

	if err := consul.Register(cfg.Port); err != nil {
		log.Printf("WARNING: consul registration failed: %v", err)
	}

	httpSrv := &http.Server{Addr: ":" + strconv.Itoa(cfg.Port), Handler: mux}

	go func() {
		log.Printf("therm-pro-server listening on :%d", cfg.Port)
		if err := httpSrv.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	consul.Deregister()
	httpSrv.Shutdown(context.Background())
}
```

**Step 4: Run tests**

Run: `make test`
Expected: All tests PASS.

**Step 5: Commit**

```bash
git add internal/api/ internal/config/ cmd/therm-pro-server/main.go
git commit -m "refactor: accept Config struct in NewServer instead of individual params"
```

---

### Task 4: Create auth middleware (network check)

**Files:**
- Create: `internal/auth/middleware.go`
- Create: `internal/auth/middleware_test.go`

**Step 1: Write the failing tests**

```go
// internal/auth/middleware_test.go
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsHomeNetwork_DirectConnection(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		cidr       string
		want       bool
	}{
		{"match", "192.168.1.50:12345", "192.168.1.0/24", true},
		{"no match", "10.0.0.1:12345", "192.168.1.0/24", false},
		{"tailscale", "100.64.1.5:12345", "100.64.0.0/10", true},
		{"loopback", "127.0.0.1:12345", "192.168.1.0/24", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remoteAddr
			got := IsHomeNetwork(r, tt.cidr, false)
			if got != tt.want {
				t.Errorf("IsHomeNetwork(%s, %s) = %v, want %v", tt.remoteAddr, tt.cidr, got, tt.want)
			}
		})
	}
}

func TestIsHomeNetwork_TrustedProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "172.17.0.1:12345" // proxy IP
	r.Header.Set("X-Forwarded-For", "192.168.1.50, 172.17.0.1")

	if !IsHomeNetwork(r, "192.168.1.0/24", true) {
		t.Error("expected home network via X-Forwarded-For")
	}
}

func TestIsHomeNetwork_UntrustedProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "8.8.8.8:12345"
	r.Header.Set("X-Forwarded-For", "192.168.1.50")

	if IsHomeNetwork(r, "192.168.1.0/24", false) {
		t.Error("should not trust X-Forwarded-For when trust_proxy is false")
	}
}

func TestRequireAuth_HomeNetworkAllowed(t *testing.T) {
	handler := RequireAuth("192.168.1.0/24", false, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("POST", "/api/session/reset", nil)
	r.RemoteAddr = "192.168.1.50:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRequireAuth_Denied(t *testing.T) {
	handler := RequireAuth("192.168.1.0/24", false, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest("POST", "/api/session/reset", nil)
	r.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
```

**Step 2: Run to verify failure**

Run: `make test`
Expected: FAIL — `auth` package doesn't exist.

**Step 3: Implement middleware**

```go
// internal/auth/middleware.go
package auth

import (
	"net"
	"net/http"
	"strings"
)

// IsHomeNetwork checks if the request originates from the allowed CIDR range.
func IsHomeNetwork(r *http.Request, cidr string, trustProxy bool) bool {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}

	var ipStr string
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// First IP in X-Forwarded-For is the original client
			ipStr = strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}

	if ipStr == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return false
		}
		ipStr = host
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return network.Contains(ip)
}

// SessionValidator is a function that checks if a request has a valid session cookie.
// Returns true if authenticated. Nil means no session validation (network-only mode).
type SessionValidator func(r *http.Request) bool

// RequireAuth returns middleware that allows requests from the home network
// or with a valid session cookie. Returns 401 otherwise.
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
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
}

// RequireHomeNetwork returns middleware that only allows requests from the home network.
// Used for passkey registration.
func RequireHomeNetwork(cidr string, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsHomeNetwork(r, cidr, trustProxy) {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}
```

**Step 4: Run tests**

Run: `make test`
Expected: All PASS.

**Step 5: Commit**

```bash
git add internal/auth/
git commit -m "feat: add auth middleware with CIDR-based home network check"
```

---

### Task 5: Create auth status endpoint and role detection

**Files:**
- Create: `internal/auth/status.go`
- Create: `internal/auth/status_test.go`

**Step 1: Write the failing test**

```go
// internal/auth/status_test.go
package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthStatus_HomeNetwork(t *testing.T) {
	handler := StatusHandler("192.168.1.0/24", false, nil)
	r := httptest.NewRequest("GET", "/api/auth/status", nil)
	r.RemoteAddr = "192.168.1.50:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	var resp struct{ Role string `json:"role"` }
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Role != "admin" {
		t.Errorf("expected admin, got %s", resp.Role)
	}
}

func TestAuthStatus_Public(t *testing.T) {
	handler := StatusHandler("192.168.1.0/24", false, nil)
	r := httptest.NewRequest("GET", "/api/auth/status", nil)
	r.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	var resp struct{ Role string `json:"role"` }
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Role != "viewer" {
		t.Errorf("expected viewer, got %s", resp.Role)
	}
}

func TestAuthStatus_HomeNetworkCanRegister(t *testing.T) {
	handler := StatusHandler("192.168.1.0/24", false, nil)
	r := httptest.NewRequest("GET", "/api/auth/status", nil)
	r.RemoteAddr = "192.168.1.50:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	var resp struct {
		Role        string `json:"role"`
		CanRegister bool   `json:"can_register"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.CanRegister {
		t.Error("expected can_register true on home network")
	}
}
```

**Step 2: Run to verify failure**

Run: `make test`
Expected: FAIL — `StatusHandler` not defined.

**Step 3: Implement**

```go
// internal/auth/status.go
package auth

import (
	"encoding/json"
	"net/http"
)

type StatusResponse struct {
	Role        string `json:"role"`
	CanRegister bool   `json:"can_register"`
}

// StatusHandler returns an http.HandlerFunc that reports the caller's access level.
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

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
```

**Step 4: Run tests**

Run: `make test`
Expected: All PASS.

**Step 5: Commit**

```bash
git add internal/auth/status.go internal/auth/status_test.go
git commit -m "feat: add auth status endpoint for role detection"
```

---

### Task 6: Wire middleware into routes

**Files:**
- Modify: `internal/api/routes.go` (wrap protected routes, add auth endpoints)

**Step 1: Update routes.go**

Replace the protected route registrations:

```go
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Auth middleware
	requireAuth := auth.RequireAuth(s.config.AllowedCIDR, s.config.TrustProxy, nil)
	requireHome := auth.RequireHomeNetwork(s.config.AllowedCIDR, s.config.TrustProxy)

	// Public API
	mux.HandleFunc("POST /api/data", s.handlePostData)
	mux.HandleFunc("GET /api/session", s.handleGetSession)
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
	mux.HandleFunc("GET /api/firmware/latest", s.firmware.HandleLatest)
	mux.HandleFunc("GET /api/firmware/download", s.firmware.HandleDownload)
	mux.HandleFunc("GET /diagnostics", s.handleDiagnostics)
	// ... (other public routes unchanged)

	// Protected API (home network or authenticated)
	mux.Handle("POST /api/session/reset", requireAuth(http.HandlerFunc(s.handleResetSession)))
	mux.Handle("POST /api/alerts", requireAuth(http.HandlerFunc(s.handlePostAlerts)))
	mux.Handle("POST /api/firmware/upload", requireAuth(http.HandlerFunc(s.firmware.HandleUpload)))

	// Auth endpoints
	mux.HandleFunc("GET /api/auth/status", auth.StatusHandler(s.config.AllowedCIDR, s.config.TrustProxy, nil))

	// Passkey registration (home network only)
	// mux.Handle("POST /auth/register/begin", requireHome(...))  -- Task 7
	// mux.Handle("POST /auth/register/finish", requireHome(...)) -- Task 7

	// Passkey login (public)
	// mux.HandleFunc("POST /auth/login/begin", ...)  -- Task 7
	// mux.HandleFunc("POST /auth/login/finish", ...) -- Task 7

	// ... rest of routes unchanged
}
```

Note: `requireHome` is declared here for future use (Task 7). The `nil` session validator will be replaced with a real one in Task 7.

**Step 2: Write integration test**

Add to `internal/api/handlers_test.go`:

```go
func TestResetSession_Unauthorized(t *testing.T) {
	cfg := testConfig()
	cfg.AllowedCIDR = "192.168.1.0/24"
	srv := NewServer(cfg, "test")
	mux := srv.Routes()

	req := httptest.NewRequest("POST", "/api/session/reset", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestResetSession_HomeNetwork(t *testing.T) {
	cfg := testConfig()
	cfg.AllowedCIDR = "192.168.1.0/24"
	srv := NewServer(cfg, "test")
	mux := srv.Routes()

	req := httptest.NewRequest("POST", "/api/session/reset", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestPostAlerts_Unauthorized(t *testing.T) {
	cfg := testConfig()
	cfg.AllowedCIDR = "192.168.1.0/24"
	srv := NewServer(cfg, "test")
	mux := srv.Routes()

	body := `{"probe_id":2,"alert":{"target_temp":203.0}}`
	req := httptest.NewRequest("POST", "/api/alerts", bytes.NewBufferString(body))
	req.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthStatus_Endpoint(t *testing.T) {
	cfg := testConfig()
	srv := NewServer(cfg, "test")
	mux := srv.Routes()

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp struct{ Role string `json:"role"` }
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Role != "admin" {
		t.Errorf("expected admin, got %s", resp.Role)
	}
}
```

**Step 3: Run tests**

Run: `make test`
Expected: All PASS.

**Step 4: Commit**

```bash
git add internal/api/
git commit -m "feat: wire auth middleware into protected routes"
```

---

### Task 7: WebAuthn passkey registration and login

**Files:**
- Create: `internal/auth/webauthn.go`
- Create: `internal/auth/storage.go`
- Create: `internal/auth/session.go`
- Create: `internal/auth/webauthn_test.go`
- Modify: `internal/api/routes.go` (uncomment passkey routes, wire session validator)

**Step 1: Write credential storage tests**

```go
// internal/auth/webauthn_test.go (storage portion)
package auth

import (
	"path/filepath"
	"testing"
)

func TestCredentialStorage_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passkeys.json")

	store := NewCredentialStore(path)
	cred := StoredCredential{
		ID:        []byte("test-credential-id"),
		PublicKey: []byte("test-public-key"),
		Label:     "My 1Password Key",
	}
	store.Add(cred)
	if err := store.Save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	store2 := NewCredentialStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(store2.Credentials()) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(store2.Credentials()))
	}
}

func TestCredentialStorage_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passkeys.json")

	store := NewCredentialStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("load of nonexistent file should not error: %v", err)
	}
	if len(store.Credentials()) != 0 {
		t.Fatalf("expected 0 credentials, got %d", len(store.Credentials()))
	}
}
```

**Step 2: Run to verify failure**

Run: `make test`
Expected: FAIL — types not defined.

**Step 3: Implement credential storage**

```go
// internal/auth/storage.go
package auth

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type StoredCredential struct {
	ID        []byte    `json:"id"`
	PublicKey  []byte    `json:"public_key"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

type CredentialStore struct {
	mu    sync.RWMutex
	path  string
	creds []StoredCredential
}

func NewCredentialStore(path string) *CredentialStore {
	return &CredentialStore{path: path}
}

func (s *CredentialStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.creds = nil
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &s.creds)
}

func (s *CredentialStore) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

func (s *CredentialStore) Add(cred StoredCredential) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cred.CreatedAt.IsZero() {
		cred.CreatedAt = time.Now()
	}
	s.creds = append(s.creds, cred)
}

func (s *CredentialStore) Credentials() []StoredCredential {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.creds
}
```

**Step 4: Run tests**

Run: `make test`
Expected: Storage tests PASS.

**Step 5: Implement WebAuthn handlers and session management**

This is the most complex task. The `go-webauthn/webauthn` library handles the crypto. Key implementation points:

- `internal/auth/webauthn.go` — `WebAuthnHandler` struct holding the `webauthn.WebAuthn` instance, credential store, and in-flight challenge sessions (map with mutex, short TTL)
- `internal/auth/session.go` — HMAC-signed cookie with expiry. Cookie name `therm_pro_session`, 24h max-age. Session secret auto-generated and persisted to `~/.therm-pro/session_secret` on first run.
- Registration: `BeginRegistration` returns challenge JSON, `FinishRegistration` validates and stores credential
- Login: `BeginLogin` returns challenge JSON, `FinishLogin` validates and sets session cookie
- The `WebAuthnHandler` exposes a `ValidateSession(r *http.Request) bool` method that plugs into `RequireAuth` as the `SessionValidator`

**Step 6: Wire into routes**

Update `internal/api/routes.go` to:
- Create `WebAuthnHandler` in `Routes()`
- Pass `webauthnHandler.ValidateSession` as the `SessionValidator` to `RequireAuth` and `StatusHandler`
- Register the four auth endpoints with appropriate middleware

**Step 7: Run tests**

Run: `make test`
Expected: All PASS.

**Step 8: Commit**

```bash
git add internal/auth/
git commit -m "feat: add WebAuthn passkey registration and login"
```

---

### Task 8: Dashboard UI — auth status and conditional controls

**Files:**
- Modify: `internal/web/static/app.js`
- Modify: `internal/web/static/index.html`
- Modify: `internal/web/static/style.css`

**Step 1: Add auth status check to app.js**

On page load, fetch `/api/auth/status` and store the role:

```javascript
let userRole = 'viewer';
let canRegister = false;

async function checkAuth() {
    const resp = await fetch('/api/auth/status');
    const data = await resp.json();
    userRole = data.role;
    canRegister = data.can_register;
    applyRoleUI();
}
```

**Step 2: Implement applyRoleUI()**

```javascript
function applyRoleUI() {
    // Hide/show reset button
    const resetBtn = document.getElementById('reset-cook');
    if (resetBtn) resetBtn.style.display = userRole === 'admin' ? '' : 'none';

    // Hide/show sign-in link
    const signInLink = document.getElementById('sign-in');
    if (signInLink) signInLink.style.display = userRole === 'admin' ? 'none' : '';

    // Hide/show register passkey link
    const registerLink = document.getElementById('register-passkey');
    if (registerLink) registerLink.style.display = canRegister ? '' : 'none';
}
```

**Step 3: Guard probe card tap-to-edit**

In the probe card click handler, check role before opening the edit modal:

```javascript
if (userRole !== 'admin') return; // read-only, ignore click
```

**Step 4: Add WebAuthn sign-in flow**

```javascript
async function signIn() {
    const beginResp = await fetch('/auth/login/begin', { method: 'POST' });
    const options = await beginResp.json();

    // Browser/1Password handles this
    const credential = await navigator.credentials.get({ publicKey: options.publicKey });

    const finishResp = await fetch('/auth/login/finish', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(credential),
    });

    if (finishResp.ok) {
        location.reload(); // Reload with new session cookie
    }
}
```

**Step 5: Add passkey registration flow (similar pattern)**

```javascript
async function registerPasskey() {
    const beginResp = await fetch('/auth/register/begin', { method: 'POST' });
    const options = await beginResp.json();
    const credential = await navigator.credentials.create({ publicKey: options.publicKey });

    const finishResp = await fetch('/auth/register/finish', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(credential),
    });

    if (finishResp.ok) {
        alert('Passkey registered!');
    }
}
```

**Step 6: Update index.html**

Add to the nav bar area:

```html
<button id="sign-in" style="display:none" onclick="signIn()">Sign In</button>
<button id="register-passkey" style="display:none" onclick="registerPasskey()">Register Passkey</button>
```

**Step 7: Update style.css**

Add styles for auth buttons matching existing nav button styles.

**Step 8: Manual test**

Run: `make run`
- Visit from `127.0.0.1` — should see admin controls (127.0.0.1 won't match default CIDR, so set `THERM_PRO_ALLOWED_CIDR=127.0.0.0/8` for local testing)
- Visit from a non-matching IP — should see read-only view with "Sign In" link

**Step 9: Commit**

```bash
git add internal/web/static/
git commit -m "feat: add read-only public view with sign-in and passkey registration UI"
```

---

### Task 9: End-to-end testing and cleanup

**Files:**
- Modify: `internal/api/handlers_test.go` (add passkey auth integration tests)

**Step 1: Add integration tests**

Test the full flow through the mux:
- Unauthenticated request to protected endpoint → 401
- Home network request to protected endpoint → 200
- Auth status returns correct role for each scenario
- Registration endpoint blocked from non-home-network → 403

**Step 2: Run full test suite**

Run: `make test`
Expected: All PASS.

**Step 3: Build**

Run: `make build`
Expected: Binary builds successfully.

**Step 4: Commit**

```bash
git add internal/api/handlers_test.go
git commit -m "test: add end-to-end access control integration tests"
```

---

Plan complete and saved to `docs/plans/2026-04-03-access-control-implementation.md`. Two execution options:

**1. Subagent-Driven (this session)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** — Open a new session in a worktree with the executing-plans skill, batch execution with checkpoints

Which approach?