package slack

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stahnma/therm-pro/internal/cook"
)

func TestSendAlert(t *testing.T) {
	var received webhookPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	alert := cook.Alert{
		ProbeID: 2,
		Label:   "Brisket",
		Type:    cook.AlertTargetReached,
		Message: "Brisket (Probe 2) hit target: 203.0°F",
		Temp:    203.0,
	}
	allTemps := [4]float64{250.0, 203.0, 180.0, -999.0}

	err := c.SendAlert(alert, allTemps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received.Text == "" {
		t.Fatal("expected non-empty message")
	}
}

func TestSendAlertNoWebhook(t *testing.T) {
	c := NewClient("")
	err := c.SendAlert(cook.Alert{}, [4]float64{})
	if err != nil {
		t.Fatal("should silently no-op when webhook is empty")
	}
}
