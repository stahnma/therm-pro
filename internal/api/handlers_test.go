package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
