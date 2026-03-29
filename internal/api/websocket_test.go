// internal/api/websocket_test.go
package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stahnma/therm-pro/internal/cook"
)

func TestWebSocketReceivesReading(t *testing.T) {
	srv := NewServer(":0", "", "", "", "")
	mux := srv.Routes()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/api/ws"
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("could not connect: %v", err)
	}
	defer ws.Close()

	// Give the connection a moment to register
	time.Sleep(50 * time.Millisecond)

	// Simulate a reading
	reading := cook.Reading{
		Timestamp: time.Now(),
		Temps:     [4]float64{250, 165, 180, 190},
	}
	srv.broadcast(reading)

	ws.SetReadDeadline(time.Now().Add(1 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	var received cook.Reading
	json.Unmarshal(msg, &received)
	if received.Temps[0] != 250 {
		t.Fatalf("expected probe 1 = 250, got %f", received.Temps[0])
	}
}
