package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stahnma/therm-pro/internal/auth"
	"github.com/stahnma/therm-pro/internal/config"
	"github.com/stahnma/therm-pro/internal/cook"
)

func TestPostData(t *testing.T) {
	srv := NewServer(&config.Config{Port: 8088, DataDir: t.TempDir()}, "test")
	body := `{"probes":[{"id":1,"temp_f":250.0},{"id":2,"temp_f":165.3},{"id":3,"temp_f":180.1},{"id":4,"temp_f":-999.0}]}`
	req := httptest.NewRequest("POST", "/api/data", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handlePostData(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestGetSession(t *testing.T) {
	srv := NewServer(&config.Config{Port: 8088, DataDir: t.TempDir()}, "test")
	req := httptest.NewRequest("GET", "/api/session", nil)
	w := httptest.NewRecorder()

	srv.handleGetSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var result struct {
		Probes [4]cook.Probe `json:"probes"`
	}
	json.NewDecoder(w.Body).Decode(&result)
	if result.Probes[0].Label == "" {
		t.Fatal("expected non-empty probe label")
	}
}

func TestResetSession(t *testing.T) {
	srv := NewServer(&config.Config{Port: 8088, DataDir: t.TempDir()}, "test")
	req := httptest.NewRequest("POST", "/api/session/reset", nil)
	w := httptest.NewRecorder()

	srv.handleResetSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestPostAlerts(t *testing.T) {
	srv := NewServer(&config.Config{Port: 8088, DataDir: t.TempDir()}, "test")
	body := `{"probe_id":2,"alert":{"target_temp":203.0}}`
	req := httptest.NewRequest("POST", "/api/alerts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handlePostAlerts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestResetSession_Unauthorized(t *testing.T) {
	srv := NewServer(&config.Config{
		Port:    8088,
		DataDir: t.TempDir(),
	}, "test")
	mux := srv.Routes()

	req := httptest.NewRequest("POST", "/api/session/reset", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestResetSession_NoSession(t *testing.T) {
	srv := NewServer(&config.Config{
		Port:    8088,
		DataDir: t.TempDir(),
	}, "test")
	mux := srv.Routes()

	req := httptest.NewRequest("POST", "/api/session/reset", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without session cookie, got %d", w.Code)
	}
}

func TestPostAlerts_Unauthorized(t *testing.T) {
	srv := NewServer(&config.Config{
		Port:    8088,
		DataDir: t.TempDir(),
	}, "test")
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
	srv := NewServer(&config.Config{
		Port:    8088,
		DataDir: t.TempDir(),
	}, "test")
	mux := srv.Routes()

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.RemoteAddr = "192.168.1.50:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp struct {
		Role string `json:"role"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Role != "viewer" {
		t.Errorf("expected viewer (no session), got %s", resp.Role)
	}
}

func TestAuthStatus_Viewer(t *testing.T) {
	cfg := &config.Config{Port: 8088, DataDir: t.TempDir()}
	srv := NewServer(cfg, "test")
	mux := srv.Routes()

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp struct {
		Role string `json:"role"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Role != "viewer" {
		t.Errorf("expected viewer, got %s", resp.Role)
	}
}

func TestFirmwareUpload_Unauthorized(t *testing.T) {
	cfg := &config.Config{Port: 8088, DataDir: t.TempDir()}
	srv := NewServer(cfg, "test")
	mux := srv.Routes()

	req := httptest.NewRequest("POST", "/api/firmware/upload", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestGetSession_Public(t *testing.T) {
	cfg := &config.Config{Port: 8088, DataDir: t.TempDir()}
	srv := NewServer(cfg, "test")
	mux := srv.Routes()

	req := httptest.NewRequest("GET", "/api/session", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestDiagnostics_Public(t *testing.T) {
	cfg := &config.Config{Port: 8088, DataDir: t.TempDir()}
	srv := NewServer(cfg, "test")
	mux := srv.Routes()

	req := httptest.NewRequest("GET", "/diagnostics", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestRegister_NoPIN(t *testing.T) {
	cfg := &config.Config{
		Port: 8088, DataDir: t.TempDir(),
		WebAuthnOrigin:  "http://localhost:8088",
		RegistrationPIN: "1234",
	}
	srv := NewServer(cfg, "test")
	mux := srv.Routes()

	// Request without PIN header should be rejected
	req := httptest.NewRequest("POST", "/auth/register/begin", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

func TestResetSession_WithSessionCookie(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Port: 8088, DataDir: dir,
		WebAuthnOrigin: "http://localhost:8088",
	}
	srv := NewServer(cfg, "test")
	mux := srv.Routes()

	// Create a valid session cookie using the server's session secret
	secret, err := auth.LoadOrCreateSessionSecret(dir)
	if err != nil {
		t.Fatalf("failed to load session secret: %v", err)
	}

	// Get the cookie by setting it on a recorder
	rec := httptest.NewRecorder()
	auth.SetSessionCookie(rec, secret)
	cookies := rec.Result().Cookies()

	// Now use that cookie on a request with a valid session
	req := httptest.NewRequest("POST", "/api/session/reset", nil)
	req.RemoteAddr = "8.8.8.8:12345"
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 with valid session cookie, got %d", w.Code)
	}
}
