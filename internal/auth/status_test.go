package auth

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestAuthStatus_HomeNetwork(t *testing.T) {
	handler := StatusHandler("192.168.1.0/24", false, nil)
	r := httptest.NewRequest("GET", "/api/auth/status", nil)
	r.RemoteAddr = "192.168.1.50:12345"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	var resp struct {
		Role string `json:"role"`
	}
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

	var resp struct {
		Role string `json:"role"`
	}
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
