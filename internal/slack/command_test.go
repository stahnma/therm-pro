package slack

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stahnma/therm-pro/internal/cook"
)

func TestFormatStatusText(t *testing.T) {
	session := cook.NewSession()
	session.Probes[0].Label = "Pit"
	session.Probes[1].Label = "Brisket"
	session.AddReading(cook.Reading{
		Timestamp: time.Now(),
		Temps:     [4]float64{275.0, 195.0, cook.ProbeDisconnected, cook.ProbeDisconnected},
		Battery:   87,
	})

	text := FormatStatusText(session)

	if !strings.Contains(text, "Pit") {
		t.Error("expected probe label 'Pit' in output")
	}
	if !strings.Contains(text, "275.0") {
		t.Error("expected temperature 275.0 in output")
	}
	if !strings.Contains(text, "87%") {
		t.Error("expected battery percentage in output")
	}
	if !strings.Contains(text, "disconnected") {
		t.Error("expected 'disconnected' for inactive probes")
	}
}

func TestHandleSlackCommand_NoSigningSecret(t *testing.T) {
	handler := NewCommandHandler("", "", nil)
	req := httptest.NewRequest("POST", "/slack/command", strings.NewReader("text=test"))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
