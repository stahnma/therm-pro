package slack

import (
	"encoding/json"
	"io"
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

func TestUploadFileAndPost_SendsFile(t *testing.T) {
	var uploadedFile []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/files.getUploadURLExternal" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true,"upload_url":"` + "http://" + r.Host + `/upload","file_id":"F123"}`))
			return
		}
		if r.URL.Path == "/upload" {
			uploadedFile, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.URL.Path == "/api/files.completeUploadExternal" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"ok":true}`))
			return
		}
	}))
	defer server.Close()

	c := NewClient("")
	c.botToken = "xoxb-test"
	c.slackAPIBase = server.URL

	pngData := []byte("fake-png-data")
	err := c.UploadFileAndPost(pngData, "C123", "test message")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(uploadedFile) != "fake-png-data" {
		t.Fatalf("expected uploaded file data, got: %s", string(uploadedFile))
	}
}

func TestUploadFileAndPost_NoBotToken(t *testing.T) {
	c := NewClient("")
	err := c.UploadFileAndPost([]byte("data"), "C123", "msg")
	if err == nil {
		t.Fatal("expected error when bot token is empty")
	}
}
