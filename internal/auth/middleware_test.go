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
	r.RemoteAddr = "172.17.0.1:12345"
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
