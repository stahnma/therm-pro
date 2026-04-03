package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthStatus_Authenticated(t *testing.T) {
	validator := func(r *http.Request) bool { return true }
	handler := StatusHandler(validator)
	r := httptest.NewRequest("GET", "/api/auth/status", nil)
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

func TestAuthStatus_Unauthenticated(t *testing.T) {
	validator := func(r *http.Request) bool { return false }
	handler := StatusHandler(validator)
	r := httptest.NewRequest("GET", "/api/auth/status", nil)
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

func TestAuthStatus_CanRegisterFalse(t *testing.T) {
	handler := StatusHandler(nil)
	r := httptest.NewRequest("GET", "/api/auth/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	var resp struct {
		Role        string `json:"role"`
		CanRegister bool   `json:"can_register"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.CanRegister {
		t.Error("expected can_register false")
	}
}
