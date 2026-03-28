# Therm-Pro Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a system that bridges a ThermPro TP25 BBQ thermometer over WiFi via an ESP32, with a Go server providing a web dashboard and Slack alerts.

**Architecture:** ESP32 connects to TP25 over BLE, decodes temperature data, and POSTs JSON to a Go server over WiFi. The Go server stores the current cook session, serves a real-time web dashboard via WebSocket, and sends Slack webhook notifications when alert thresholds are crossed.

**Tech Stack:** Go (server), Arduino/C++ with NimBLE + PlatformIO (ESP32), vanilla HTML/CSS/JS with uPlot (dashboard), swaggo/swag (API docs)

---

## Task 1: Go Module and Project Skeleton

**Files:**
- Create: `go.mod`
- Create: `cmd/therm-pro-server/main.go`
- Create: `Makefile`

**Step 1: Initialize Go module**

Run: `go mod init github.com/stahnma/therm-pro`

**Step 2: Create entry point**

```go
// cmd/therm-pro-server/main.go
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	log.Printf("therm-pro-server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
```

**Step 3: Create Makefile**

```makefile
.PHONY: build run clean swag

build:
	go build -o bin/therm-pro-server ./cmd/therm-pro-server

run:
	go run ./cmd/therm-pro-server

clean:
	rm -rf bin/

swag:
	swag init -g cmd/therm-pro-server/main.go -o internal/api/docs
```

**Step 4: Verify it builds and runs**

Run: `make build && ./bin/therm-pro-server &`
Run: `curl http://localhost:8080/healthz`
Expected: `ok`

**Step 5: Commit**

```bash
git add go.mod cmd/ Makefile
git commit -m "feat: initialize Go project skeleton with health endpoint"
```

---

## Task 2: Cook Session Data Model

**Files:**
- Create: `internal/cook/session.go`
- Create: `internal/cook/session_test.go`

**Step 1: Write the failing tests**

```go
// internal/cook/session_test.go
package cook

import (
	"testing"
	"time"
)

func TestNewSession(t *testing.T) {
	s := NewSession()
	if len(s.Probes) != 4 {
		t.Fatalf("expected 4 probes, got %d", len(s.Probes))
	}
	if s.Probes[0].Label != "Probe 1" {
		t.Fatalf("expected default label 'Probe 1', got %q", s.Probes[0].Label)
	}
}

func TestAddReading(t *testing.T) {
	s := NewSession()
	r := Reading{
		Timestamp: time.Now(),
		Temps:     [4]float64{250.0, 165.3, 180.1, -999.0},
	}
	s.AddReading(r)
	if len(s.History) != 1 {
		t.Fatalf("expected 1 reading, got %d", len(s.History))
	}
	if s.Probes[0].CurrentTemp != 250.0 {
		t.Fatalf("expected probe 1 temp 250.0, got %f", s.Probes[0].CurrentTemp)
	}
}

func TestAddReadingDisconnectedProbe(t *testing.T) {
	s := NewSession()
	r := Reading{
		Timestamp: time.Now(),
		Temps:     [4]float64{250.0, -999.0, -999.0, -999.0},
	}
	s.AddReading(r)
	if s.Probes[1].Connected {
		t.Fatal("probe 2 should be disconnected for -999.0")
	}
	if !s.Probes[0].Connected {
		t.Fatal("probe 1 should be connected")
	}
}

func TestResetSession(t *testing.T) {
	s := NewSession()
	s.Probes[0].Label = "Pit"
	s.AddReading(Reading{Timestamp: time.Now(), Temps: [4]float64{250, 165, 180, 190}})
	s.Reset()
	if len(s.History) != 0 {
		t.Fatal("history should be empty after reset")
	}
	// Labels should be preserved across reset
	if s.Probes[0].Label != "Pit" {
		t.Fatalf("expected label 'Pit' preserved, got %q", s.Probes[0].Label)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cook/ -v`
Expected: FAIL (types not defined)

**Step 3: Write implementation**

```go
// internal/cook/session.go
package cook

import (
	"sync"
	"time"
)

const (
	ProbeDisconnected = -999.0
	ProbeError        = -100.0
	ProbeOverTemp     = 666.0
	NumProbes         = 4
)

type Reading struct {
	Timestamp time.Time    `json:"timestamp"`
	Temps     [4]float64   `json:"temps"`
	Battery   int          `json:"battery"`
}

type AlertConfig struct {
	TargetTemp *float64 `json:"target_temp,omitempty"`
	HighTemp   *float64 `json:"high_temp,omitempty"`
	LowTemp    *float64 `json:"low_temp,omitempty"`
}

type Probe struct {
	ID          int         `json:"id"`
	Label       string      `json:"label"`
	CurrentTemp float64     `json:"current_temp"`
	Connected   bool        `json:"connected"`
	Alert       AlertConfig `json:"alert"`
}

type Session struct {
	mu      sync.RWMutex
	Probes  [NumProbes]Probe `json:"probes"`
	History []Reading        `json:"history"`
	Started time.Time        `json:"started"`
}

func NewSession() *Session {
	s := &Session{
		Started: time.Now(),
	}
	for i := 0; i < NumProbes; i++ {
		s.Probes[i] = Probe{
			ID:    i + 1,
			Label: fmt.Sprintf("Probe %d", i+1),
		}
	}
	return s
}

func (s *Session) AddReading(r Reading) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = append(s.History, r)
	for i := 0; i < NumProbes; i++ {
		s.Probes[i].CurrentTemp = r.Temps[i]
		s.Probes[i].Connected = r.Temps[i] != ProbeDisconnected &&
			r.Temps[i] != ProbeError &&
			r.Temps[i] != ProbeOverTemp
	}
}

func (s *Session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = nil
	s.Started = time.Now()
	for i := 0; i < NumProbes; i++ {
		s.Probes[i].CurrentTemp = 0
		s.Probes[i].Connected = false
		// Preserve labels and alert config
	}
}
```

Note: Add `"fmt"` to the import list in session.go.

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/cook/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cook/
git commit -m "feat: add cook session data model with probe tracking"
```

---

## Task 3: JSON File Persistence

**Files:**
- Create: `internal/cook/persist.go`
- Create: `internal/cook/persist_test.go`

**Step 1: Write the failing test**

```go
// internal/cook/persist_test.go
package cook

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	s := NewSession()
	s.Probes[0].Label = "Pit"
	s.AddReading(Reading{Timestamp: time.Now(), Temps: [4]float64{250, 165, 180, 190}})

	if err := Save(s, path); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Probes[0].Label != "Pit" {
		t.Fatalf("expected label 'Pit', got %q", loaded.Probes[0].Label)
	}
	if len(loaded.History) != 1 {
		t.Fatalf("expected 1 reading, got %d", len(loaded.History))
	}
}

func TestLoadMissingFile(t *testing.T) {
	s, err := Load("/nonexistent/path.json")
	if err != nil {
		t.Fatalf("load of missing file should not error, got: %v", err)
	}
	if len(s.Probes) != 4 {
		t.Fatal("should return fresh session for missing file")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/cook/ -v -run TestSaveAndLoad`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/cook/persist.go
package cook

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

func Save(s *Session, path string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func Load(path string) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return NewSession(), nil
		}
		return nil, err
	}
	s := &Session{}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, err
	}
	return s, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/cook/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cook/persist.go internal/cook/persist_test.go
git commit -m "feat: add JSON file persistence for cook sessions"
```

---

## Task 4: Alert Engine

**Files:**
- Create: `internal/cook/alerts.go`
- Create: `internal/cook/alerts_test.go`

**Step 1: Write the failing tests**

```go
// internal/cook/alerts_test.go
package cook

import (
	"testing"
	"time"
)

func floatPtr(f float64) *float64 { return &f }

func TestAlertTargetReached(t *testing.T) {
	e := NewAlertEngine()
	probe := Probe{
		ID:          2,
		Label:       "Brisket",
		CurrentTemp: 203.0,
		Connected:   true,
		Alert:       AlertConfig{TargetTemp: floatPtr(203.0)},
	}
	alerts := e.Evaluate(probe)
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}
	if alerts[0].Type != AlertTargetReached {
		t.Fatalf("expected TargetReached, got %v", alerts[0].Type)
	}
}

func TestAlertNotRepeated(t *testing.T) {
	e := NewAlertEngine()
	probe := Probe{
		ID:          2,
		Label:       "Brisket",
		CurrentTemp: 203.0,
		Connected:   true,
		Alert:       AlertConfig{TargetTemp: floatPtr(203.0)},
	}
	_ = e.Evaluate(probe) // first time fires
	alerts := e.Evaluate(probe) // second time should not
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts (de-dup), got %d", len(alerts))
	}
}

func TestAlertResetsAfterHysteresis(t *testing.T) {
	e := NewAlertEngine()
	target := 203.0
	probe := Probe{
		ID: 2, Label: "Brisket", CurrentTemp: 203.0, Connected: true,
		Alert: AlertConfig{TargetTemp: &target},
	}
	_ = e.Evaluate(probe) // fires

	// Drop below hysteresis
	probe.CurrentTemp = 199.0
	_ = e.Evaluate(probe) // resets

	// Rise again
	probe.CurrentTemp = 203.0
	alerts := e.Evaluate(probe)
	if len(alerts) != 1 {
		t.Fatalf("expected alert to fire again after hysteresis reset, got %d", len(alerts))
	}
}

func TestAlertRateLimited(t *testing.T) {
	e := NewAlertEngine()
	e.minInterval = 1 * time.Hour // exaggerate for test
	target := 203.0
	probe := Probe{
		ID: 2, Label: "Brisket", CurrentTemp: 203.0, Connected: true,
		Alert: AlertConfig{TargetTemp: &target},
	}
	_ = e.Evaluate(probe)

	// Reset hysteresis manually so it would fire
	probe.CurrentTemp = 199.0
	_ = e.Evaluate(probe)
	probe.CurrentTemp = 203.0
	alerts := e.Evaluate(probe)
	if len(alerts) != 0 {
		t.Fatalf("expected rate-limited (0 alerts), got %d", len(alerts))
	}
}

func TestAlertHighLow(t *testing.T) {
	e := NewAlertEngine()
	probe := Probe{
		ID: 1, Label: "Pit", CurrentTemp: 210.0, Connected: true,
		Alert: AlertConfig{LowTemp: floatPtr(225.0), HighTemp: floatPtr(275.0)},
	}
	alerts := e.Evaluate(probe)
	if len(alerts) != 1 {
		t.Fatalf("expected 1 low-temp alert, got %d", len(alerts))
	}
	if alerts[0].Type != AlertLowTemp {
		t.Fatalf("expected LowTemp, got %v", alerts[0].Type)
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/cook/ -v -run TestAlert`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/cook/alerts.go
package cook

import (
	"fmt"
	"sync"
	"time"
)

const (
	AlertTargetReached = "target_reached"
	AlertHighTemp      = "high_temp"
	AlertLowTemp       = "low_temp"
	DefaultHysteresis  = 3.0
)

type Alert struct {
	ProbeID   int       `json:"probe_id"`
	Label     string    `json:"label"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Temp      float64   `json:"temp"`
	Threshold float64   `json:"threshold"`
	Time      time.Time `json:"time"`
}

type probeAlertState struct {
	fired    map[string]bool
	lastSent map[string]time.Time
}

type AlertEngine struct {
	mu          sync.Mutex
	state       map[int]*probeAlertState
	hysteresis  float64
	minInterval time.Duration
}

func NewAlertEngine() *AlertEngine {
	return &AlertEngine{
		state:       make(map[int]*probeAlertState),
		hysteresis:  DefaultHysteresis,
		minInterval: 60 * time.Second,
	}
}

func (e *AlertEngine) getState(probeID int) *probeAlertState {
	if _, ok := e.state[probeID]; !ok {
		e.state[probeID] = &probeAlertState{
			fired:    make(map[string]bool),
			lastSent: make(map[string]time.Time),
		}
	}
	return e.state[probeID]
}

func (e *AlertEngine) Evaluate(p Probe) []Alert {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !p.Connected {
		return nil
	}

	var alerts []Alert
	st := e.getState(p.ID)
	now := time.Now()

	check := func(alertType string, condition bool, resetCondition bool, threshold float64) {
		if resetCondition {
			st.fired[alertType] = false
		}
		if condition && !st.fired[alertType] {
			if last, ok := st.lastSent[alertType]; ok && now.Sub(last) < e.minInterval {
				return
			}
			st.fired[alertType] = true
			st.lastSent[alertType] = now
			alerts = append(alerts, Alert{
				ProbeID:   p.ID,
				Label:     p.Label,
				Type:      alertType,
				Message:   formatAlertMessage(p, alertType, threshold),
				Temp:      p.CurrentTemp,
				Threshold: threshold,
				Time:      now,
			})
		}
	}

	if p.Alert.TargetTemp != nil {
		target := *p.Alert.TargetTemp
		check(AlertTargetReached,
			p.CurrentTemp >= target,
			p.CurrentTemp < target-e.hysteresis,
			target)
	}
	if p.Alert.HighTemp != nil {
		high := *p.Alert.HighTemp
		check(AlertHighTemp,
			p.CurrentTemp > high,
			p.CurrentTemp <= high-e.hysteresis,
			high)
	}
	if p.Alert.LowTemp != nil {
		low := *p.Alert.LowTemp
		check(AlertLowTemp,
			p.CurrentTemp < low,
			p.CurrentTemp >= low+e.hysteresis,
			low)
	}

	return alerts
}

func formatAlertMessage(p Probe, alertType string, threshold float64) string {
	switch alertType {
	case AlertTargetReached:
		return fmt.Sprintf("%s (Probe %d) hit target: %.1f°F", p.Label, p.ID, p.CurrentTemp)
	case AlertHighTemp:
		return fmt.Sprintf("%s (Probe %d) above %.1f°F: currently %.1f°F", p.Label, p.ID, threshold, p.CurrentTemp)
	case AlertLowTemp:
		return fmt.Sprintf("%s (Probe %d) below %.1f°F: currently %.1f°F", p.Label, p.ID, threshold, p.CurrentTemp)
	default:
		return fmt.Sprintf("%s (Probe %d): %.1f°F", p.Label, p.ID, p.CurrentTemp)
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/cook/ -v -run TestAlert`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/cook/alerts.go internal/cook/alerts_test.go
git commit -m "feat: add alert engine with hysteresis and rate limiting"
```

---

## Task 5: Slack Notification Client

**Files:**
- Create: `internal/slack/slack.go`
- Create: `internal/slack/slack_test.go`

**Step 1: Write the failing test**

```go
// internal/slack/slack_test.go
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
```

**Step 2: Run to verify failure**

Run: `go test ./internal/slack/ -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/slack/slack.go
package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stahnma/therm-pro/internal/cook"
)

type webhookPayload struct {
	Text string `json:"text"`
}

type Client struct {
	webhookURL string
	httpClient *http.Client
}

func NewClient(webhookURL string) *Client {
	return &Client{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) SendAlert(alert cook.Alert, allTemps [4]float64) error {
	if c.webhookURL == "" {
		return nil
	}

	text := fmt.Sprintf("*%s*\nAll probes: 1=%.1f°F  2=%.1f°F  3=%.1f°F  4=%.1f°F",
		alert.Message, allTemps[0], allTemps[1], allTemps[2], allTemps[3])

	payload, _ := json.Marshal(webhookPayload{Text: text})
	resp, err := c.httpClient.Post(c.webhookURL, "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("slack webhook: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack webhook returned %d", resp.StatusCode)
	}
	return nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/slack/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/slack/
git commit -m "feat: add Slack webhook notification client"
```

---

## Task 6: HTTP API Handlers

**Files:**
- Create: `internal/api/handlers.go`
- Create: `internal/api/handlers_test.go`

**Step 1: Write the failing tests**

```go
// internal/api/handlers_test.go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stahnma/therm-pro/internal/cook"
)

func TestPostData(t *testing.T) {
	srv := NewServer(":8080", "", "")
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
	srv := NewServer(":8080", "", "")
	req := httptest.NewRequest("GET", "/api/session", nil)
	w := httptest.NewRecorder()

	srv.handleGetSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var session cook.Session
	json.NewDecoder(w.Body).Decode(&session)
	if len(session.Probes) != 4 {
		t.Fatalf("expected 4 probes, got %d", len(session.Probes))
	}
}

func TestResetSession(t *testing.T) {
	srv := NewServer(":8080", "", "")
	req := httptest.NewRequest("POST", "/api/session/reset", nil)
	w := httptest.NewRecorder()

	srv.handleResetSession(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestPostAlerts(t *testing.T) {
	srv := NewServer(":8080", "", "")
	body := `{"probe_id":2,"alert":{"target_temp":203.0}}`
	req := httptest.NewRequest("POST", "/api/alerts", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.handlePostAlerts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/api/ -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/api/handlers.go
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/stahnma/therm-pro/internal/cook"
	"github.com/stahnma/therm-pro/internal/slack"
)

type Server struct {
	addr        string
	session     *cook.Session
	alerts      *cook.AlertEngine
	slack       *slack.Client
	sessionPath string
	wsClients   map[*wsClient]bool
	wsMu        sync.Mutex
}

type ProbeReading struct {
	ID    int     `json:"id"`
	TempF float64 `json:"temp_f"`
}

type DataPayload struct {
	Probes    []ProbeReading `json:"probes"`
	Timestamp string         `json:"timestamp,omitempty"`
	Battery   int            `json:"battery,omitempty"`
}

type AlertPayload struct {
	ProbeID int              `json:"probe_id"`
	Alert   cook.AlertConfig `json:"alert"`
}

func NewServer(addr, slackWebhook, sessionPath string) *Server {
	session, err := cook.Load(sessionPath)
	if err != nil {
		log.Printf("warning: could not load session: %v", err)
		session = cook.NewSession()
	}
	return &Server{
		addr:        addr,
		session:     session,
		alerts:      cook.NewAlertEngine(),
		slack:       slack.NewClient(slackWebhook),
		sessionPath: sessionPath,
		wsClients:   make(map[*wsClient]bool),
	}
}

func (s *Server) handlePostData(w http.ResponseWriter, r *http.Request) {
	var payload DataPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	var temps [4]float64
	for i := range temps {
		temps[i] = cook.ProbeDisconnected
	}
	for _, p := range payload.Probes {
		if p.ID >= 1 && p.ID <= 4 {
			temps[p.ID-1] = p.TempF
		}
	}

	reading := cook.Reading{
		Timestamp: time.Now(),
		Temps:     temps,
		Battery:   payload.Battery,
	}
	s.session.AddReading(reading)

	// Persist
	if s.sessionPath != "" {
		if err := cook.Save(s.session, s.sessionPath); err != nil {
			log.Printf("warning: could not save session: %v", err)
		}
	}

	// Evaluate alerts
	s.session.RLock()
	for i := 0; i < cook.NumProbes; i++ {
		fired := s.alerts.Evaluate(s.session.Probes[i])
		for _, alert := range fired {
			log.Printf("ALERT: %s", alert.Message)
			go s.slack.SendAlert(alert, temps)
		}
	}
	s.session.RUnlock()

	// Broadcast to WebSocket clients
	s.broadcast(reading)

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	s.session.RLock()
	defer s.session.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.session)
}

func (s *Server) handleResetSession(w http.ResponseWriter, r *http.Request) {
	s.session.Reset()
	if s.sessionPath != "" {
		cook.Save(s.session, s.sessionPath)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePostAlerts(w http.ResponseWriter, r *http.Request) {
	var payload AlertPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if payload.ProbeID < 1 || payload.ProbeID > 4 {
		http.Error(w, "probe_id must be 1-4", http.StatusBadRequest)
		return
	}
	s.session.SetAlert(payload.ProbeID, payload.Alert)
	if s.sessionPath != "" {
		cook.Save(s.session, s.sessionPath)
	}
	w.WriteHeader(http.StatusOK)
}
```

Note: This requires adding `RLock()`/`RUnlock()` and `SetAlert()` methods to `Session`. Add to `session.go`:

```go
func (s *Session) RLock()   { s.mu.RLock() }
func (s *Session) RUnlock() { s.mu.RUnlock() }

func (s *Session) SetAlert(probeID int, alert AlertConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Probes[probeID-1].Alert = alert
}
```

The `broadcast` and `wsClient` will be stubbed (empty) for now and implemented in the WebSocket task.

**Step 4: Run tests**

Run: `go test ./internal/api/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/ internal/cook/session.go
git commit -m "feat: add HTTP API handlers for data, session, and alerts"
```

---

## Task 7: WebSocket Support

**Files:**
- Create: `internal/api/websocket.go`
- Create: `internal/api/websocket_test.go`

**Step 1: Install gorilla/websocket**

Run: `go get github.com/gorilla/websocket`

**Step 2: Write the failing test**

```go
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
	srv := NewServer(":0", "", "")
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
```

**Step 3: Write implementation**

```go
// internal/api/websocket.go
package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/stahnma/therm-pro/internal/cook"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade: %v", err)
		return
	}

	client := &wsClient{
		conn: conn,
		send: make(chan []byte, 64),
	}

	s.wsMu.Lock()
	s.wsClients[client] = true
	s.wsMu.Unlock()

	go client.writePump(s)
}

func (c *wsClient) writePump(s *Server) {
	defer func() {
		s.wsMu.Lock()
		delete(s.wsClients, c)
		s.wsMu.Unlock()
		c.conn.Close()
	}()

	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func (s *Server) broadcast(reading cook.Reading) {
	data, err := json.Marshal(reading)
	if err != nil {
		return
	}

	s.wsMu.Lock()
	defer s.wsMu.Unlock()
	for client := range s.wsClients {
		select {
		case client.send <- data:
		default:
			close(client.send)
			delete(s.wsClients, client)
		}
	}
}
```

Also add the `Routes()` method to `Server`:

```go
// internal/api/routes.go
package api

import "net/http"

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/data", s.handlePostData)
	mux.HandleFunc("GET /api/session", s.handleGetSession)
	mux.HandleFunc("POST /api/session/reset", s.handleResetSession)
	mux.HandleFunc("POST /api/alerts", s.handlePostAlerts)
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	})
	return mux
}
```

**Step 4: Run tests**

Run: `go test ./internal/api/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/api/
git commit -m "feat: add WebSocket support and route registration"
```

---

## Task 8: Firmware OTA Endpoints

**Files:**
- Create: `internal/firmware/firmware.go`
- Create: `internal/firmware/firmware_test.go`

**Step 1: Write the failing tests**

```go
// internal/firmware/firmware_test.go
package firmware

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUploadAndDownload(t *testing.T) {
	store := NewStore(t.TempDir())

	// Upload
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("firmware", "firmware.bin")
	fw.Write([]byte("fake firmware data"))
	vf, _ := w.CreateFormField("version")
	vf.Write([]byte("2"))
	w.Close()

	req := httptest.NewRequest("POST", "/api/firmware/upload", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	store.HandleUpload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Check latest
	req = httptest.NewRequest("GET", "/api/firmware/latest", nil)
	rec = httptest.NewRecorder()
	store.HandleLatest(rec, req)
	var info VersionInfo
	json.NewDecoder(rec.Body).Decode(&info)
	if info.Version != 2 {
		t.Fatalf("expected version 2, got %d", info.Version)
	}

	// Download
	req = httptest.NewRequest("GET", "/api/firmware/download", nil)
	rec = httptest.NewRecorder()
	store.HandleDownload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download: expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "fake firmware data" {
		t.Fatalf("unexpected firmware content: %q", rec.Body.String())
	}
}

func TestLatestNoFirmware(t *testing.T) {
	store := NewStore(t.TempDir())
	req := httptest.NewRequest("GET", "/api/firmware/latest", nil)
	rec := httptest.NewRecorder()
	store.HandleLatest(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/firmware/ -v`
Expected: FAIL

**Step 3: Write implementation**

```go
// internal/firmware/firmware.go
package firmware

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

type VersionInfo struct {
	Version     int    `json:"version"`
	DownloadURL string `json:"download_url"`
}

type Store struct {
	mu      sync.RWMutex
	dir     string
	version int
}

func NewStore(dir string) *Store {
	os.MkdirAll(dir, 0755)
	s := &Store{dir: dir}
	// Try to load existing version
	data, err := os.ReadFile(filepath.Join(dir, "version.json"))
	if err == nil {
		var info VersionInfo
		json.Unmarshal(data, &info)
		s.version = info.Version
	}
	return s
}

func (s *Store) HandleUpload(w http.ResponseWriter, r *http.Request) {
	file, _, err := r.FormFile("firmware")
	if err != nil {
		http.Error(w, "missing firmware file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	versionStr := r.FormValue("version")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		http.Error(w, "invalid version number", http.StatusBadRequest)
		return
	}

	binPath := filepath.Join(s.dir, "firmware.bin")
	out, err := os.Create(binPath)
	if err != nil {
		http.Error(w, "could not save firmware", http.StatusInternalServerError)
		return
	}
	defer out.Close()
	io.Copy(out, file)

	s.mu.Lock()
	s.version = version
	info := VersionInfo{Version: version, DownloadURL: "/api/firmware/download"}
	data, _ := json.Marshal(info)
	os.WriteFile(filepath.Join(s.dir, "version.json"), data, 0644)
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "firmware v%d uploaded\n", version)
}

func (s *Store) HandleLatest(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.version == 0 {
		http.Error(w, "no firmware available", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(VersionInfo{
		Version:     s.version,
		DownloadURL: "/api/firmware/download",
	})
}

func (s *Store) HandleDownload(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	binPath := filepath.Join(s.dir, "firmware.bin")
	http.ServeFile(w, r, binPath)
}
```

**Step 4: Run tests**

Run: `go test ./internal/firmware/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/firmware/
git commit -m "feat: add firmware OTA upload/download/version endpoints"
```

---

## Task 9: Wire Up Main and Add Swagger

**Files:**
- Modify: `cmd/therm-pro-server/main.go`
- Create: `internal/api/routes.go` (if not already created in Task 7)

**Step 1: Install swaggo**

Run: `go install github.com/swaggo/swag/cmd/swag@latest`
Run: `go get github.com/swaggo/http-swagger`

**Step 2: Add swagger annotations to handlers**

Add `// @Summary`, `// @Tags`, `// @Accept`, `// @Produce`, `// @Success`, `// @Router` comments above each handler function in `handlers.go`. (Refer to swaggo docs for exact format.)

**Step 3: Update main.go**

```go
// cmd/therm-pro-server/main.go
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/stahnma/therm-pro/internal/api"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	slackWebhook := os.Getenv("THERM_PRO_SLACK_WEBHOOK")

	homeDir, _ := os.UserHomeDir()
	sessionPath := filepath.Join(homeDir, ".therm-pro", "session.json")

	srv := api.NewServer(":"+port, slackWebhook, sessionPath)
	mux := srv.Routes()

	log.Printf("therm-pro-server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
```

**Step 4: Generate swagger docs**

Run: `swag init -g cmd/therm-pro-server/main.go -o internal/api/docs`

**Step 5: Add swagger UI route to routes.go**

```go
import httpSwagger "github.com/swaggo/http-swagger"

// In Routes():
mux.Handle("GET /api/docs/", httpSwagger.WrapHandler)
```

**Step 6: Build and smoke test**

Run: `make build && ./bin/therm-pro-server &`
Run: `curl -s http://localhost:8080/healthz` → `ok`
Run: `curl -s -X POST http://localhost:8080/api/data -H 'Content-Type: application/json' -d '{"probes":[{"id":1,"temp_f":250}]}'`
Run: `curl -s http://localhost:8080/api/session | jq .probes[0].current_temp` → `250`

**Step 7: Commit**

```bash
git add cmd/ internal/ go.mod go.sum
git commit -m "feat: wire up main server with all routes and swagger docs"
```

---

## Task 10: Web Dashboard

**Files:**
- Create: `internal/web/static/index.html`
- Create: `internal/web/static/style.css`
- Create: `internal/web/static/app.js`
- Create: `internal/web/embed.go`

**Step 1: Create the embed file**

```go
// internal/web/embed.go
package web

import "embed"

//go:embed static/*
var StaticFiles embed.FS
```

**Step 2: Create index.html**

Mobile-first layout with 4 probe cards and a chart container. Include uPlot via CDN (will be copied locally later if needed). Structure:
- Header with title and reset button
- 2x2 grid of probe cards (each shows label, temp, target, color status)
- Chart container for uPlot time-series
- Modal/overlay for editing probe label and alert thresholds
- F/C toggle

**Step 3: Create style.css**

- CSS grid: `grid-template-columns: 1fr 1fr` with `@media (max-width: 600px) { grid-template-columns: 1fr }`
- Probe card colors: green default, amber when within 10°F of target, red when target hit
- Large font for current temperature (2rem+)
- Clean, dark theme (easier to read outdoors)

**Step 4: Create app.js**

- On load: fetch `GET /api/session` to populate initial state
- Open WebSocket to `/api/ws`
- On each message: update probe cards, append to uPlot chart
- Probe card tap: show edit modal, POST to `/api/alerts` on save
- Reset button: confirm, POST to `/api/session/reset`
- Reconnect logic: on close, show banner, retry every 3 seconds

**Step 5: Add static file serving to routes.go**

```go
import "github.com/stahnma/therm-pro/internal/web"

// In Routes():
staticFS, _ := fs.Sub(web.StaticFiles, "static")
mux.Handle("GET /", http.FileServer(http.FS(staticFS)))
```

**Step 6: Build, run, and verify in browser**

Run: `make build && ./bin/therm-pro-server`
Open: `http://localhost:8080/` in browser
Verify: 4 probe cards render, WebSocket connects

**Step 7: Commit**

```bash
git add internal/web/
git commit -m "feat: add embedded web dashboard with live WebSocket updates"
```

---

## Task 11: ESP32 PlatformIO Project Setup

**Files:**
- Create: `esp32/platformio.ini`
- Create: `esp32/src/main.cpp`
- Create: `esp32/src/config.h`

**Step 1: Create platformio.ini**

```ini
[env:esp32]
platform = espressif32
board = esp32dev
framework = arduino
lib_deps =
    h2zero/NimBLE-Arduino@^1.4.0
monitor_speed = 115200
```

**Step 2: Create config.h**

```cpp
// esp32/src/config.h
#ifndef CONFIG_H
#define CONFIG_H

#define WIFI_SSID "your-ssid"
#define WIFI_PASS "your-password"
#define SERVER_URL "http://192.168.1.100:8080"
#define FIRMWARE_VERSION 1
#define LED_PIN 2

#endif
```

**Step 3: Create main.cpp skeleton**

```cpp
// esp32/src/main.cpp
#include <Arduino.h>
#include <WiFi.h>
#include <NimBLEDevice.h>
#include <HTTPClient.h>
#include "config.h"

void setup() {
    Serial.begin(115200);
    pinMode(LED_PIN, OUTPUT);

    // Connect WiFi
    WiFi.begin(WIFI_SSID, WIFI_PASS);
    Serial.print("Connecting to WiFi");
    while (WiFi.status() != WL_CONNECTED) {
        delay(500);
        Serial.print(".");
    }
    Serial.printf("\nConnected: %s\n", WiFi.localIP().toString().c_str());
}

void loop() {
    // Placeholder
    delay(1000);
}
```

**Step 4: Verify it compiles**

Run: `cd esp32 && pio run`
Expected: BUILD SUCCESS

**Step 5: Commit**

```bash
git add esp32/
git commit -m "feat: initialize ESP32 PlatformIO project with WiFi"
```

---

## Task 12: ESP32 BLE Connection to TP25

**Files:**
- Modify: `esp32/src/main.cpp`
- Create: `esp32/src/thermopro.h`
- Create: `esp32/src/thermopro.cpp`

**Step 1: Create thermopro.h**

```cpp
#ifndef THERMOPRO_H
#define THERMOPRO_H

#include <NimBLEDevice.h>

// BLE UUIDs
#define TP25_SERVICE_UUID      "1086fff0-3343-4817-8bb2-b32206336ce8"
#define TP25_WRITE_CHAR_UUID   "1086fff1-3343-4817-8bb2-b32206336ce8"
#define TP25_NOTIFY_CHAR_UUID  "1086fff2-3343-4817-8bb2-b32206336ce8"

#define PROBE_DISCONNECTED -999.0
#define PROBE_ERROR        -100.0
#define PROBE_OVERTEMP      666.0
#define NUM_PROBES          4

struct ProbeData {
    float temps[NUM_PROBES];
    int battery;
    bool valid;
};

class ThermoPro {
public:
    bool scan();
    bool connect();
    bool sendHandshake();
    ProbeData getLatestData();
    bool isConnected();

private:
    NimBLEAdvertisedDevice* device = nullptr;
    NimBLEClient* client = nullptr;
    NimBLERemoteCharacteristic* writeChar = nullptr;
    NimBLERemoteCharacteristic* notifyChar = nullptr;

    static ProbeData latestData;
    static bool dataReady;
    static void notifyCallback(NimBLERemoteCharacteristic* pChar,
                               uint8_t* pData, size_t length, bool isNotify);
    static float decodeBCD(uint8_t byte1, uint8_t byte2);
};

#endif
```

**Step 2: Create thermopro.cpp**

```cpp
#include "thermopro.h"

ProbeData ThermoPro::latestData = {};
bool ThermoPro::dataReady = false;

float ThermoPro::decodeBCD(uint8_t byte1, uint8_t byte2) {
    // Sentinel values
    if (byte1 == 0xFF && byte2 == 0xFF) return PROBE_DISCONNECTED;
    if (byte1 == 0xDD && byte2 == 0xDD) return PROBE_ERROR;
    if (byte1 == 0xEE && byte2 == 0xEE) return PROBE_OVERTEMP;

    bool negative = (byte1 & 0x80) != 0;
    float hundreds = ((byte1 >> 4) & 0x07) * 100.0;
    float tens = (byte1 & 0x0F) * 10.0;
    float ones = (byte2 >> 4) & 0x0F;
    float tenths = (byte2 & 0x0F) * 0.1;
    float temp = hundreds + tens + ones + tenths;
    return negative ? -temp : temp;
}

void ThermoPro::notifyCallback(NimBLERemoteCharacteristic* pChar,
                                uint8_t* pData, size_t length, bool isNotify) {
    if (length < 13 || pData[0] != 0x30) return;

    latestData.battery = pData[2];
    latestData.temps[0] = decodeBCD(pData[5], pData[6]);
    latestData.temps[1] = decodeBCD(pData[7], pData[8]);
    latestData.temps[2] = decodeBCD(pData[9], pData[10]);
    latestData.temps[3] = decodeBCD(pData[11], pData[12]);
    latestData.valid = true;
    dataReady = true;
}

bool ThermoPro::scan() {
    NimBLEScan* scan = NimBLEDevice::getScan();
    scan->setActiveScan(true);
    NimBLEScanResults results = scan->start(10);

    for (int i = 0; i < results.getCount(); i++) {
        NimBLEAdvertisedDevice adv = results.getDevice(i);
        if (adv.getName() == "Thermopro") {
            device = new NimBLEAdvertisedDevice(adv);
            Serial.println("Found ThermoPro TP25");
            return true;
        }
    }
    Serial.println("ThermoPro not found");
    return false;
}

bool ThermoPro::connect() {
    if (!device) return false;

    client = NimBLEDevice::createClient();
    if (!client->connect(device)) {
        Serial.println("BLE connect failed");
        return false;
    }

    NimBLERemoteService* svc = client->getService(TP25_SERVICE_UUID);
    if (!svc) {
        Serial.println("Service not found");
        client->disconnect();
        return false;
    }

    writeChar = svc->getCharacteristic(TP25_WRITE_CHAR_UUID);
    notifyChar = svc->getCharacteristic(TP25_NOTIFY_CHAR_UUID);

    if (!writeChar || !notifyChar) {
        Serial.println("Characteristics not found");
        client->disconnect();
        return false;
    }

    notifyChar->subscribe(true, notifyCallback);
    return true;
}

bool ThermoPro::sendHandshake() {
    if (!writeChar) return false;
    uint8_t handshake[] = {0x01, 0x09, 0x70, 0x32, 0xe2, 0xc1, 0x79, 0x9d, 0xb4, 0xd1, 0xc7, 0xb1};
    return writeChar->writeValue(handshake, sizeof(handshake), false);
}

ProbeData ThermoPro::getLatestData() {
    dataReady = false;
    return latestData;
}

bool ThermoPro::isConnected() {
    return client && client->isConnected();
}
```

**Step 3: Update main.cpp to use ThermoPro class**

```cpp
#include <Arduino.h>
#include <WiFi.h>
#include <HTTPClient.h>
#include <NimBLEDevice.h>
#include "config.h"
#include "thermopro.h"

ThermoPro tp;

void setup() {
    Serial.begin(115200);
    pinMode(LED_PIN, OUTPUT);

    // WiFi
    WiFi.begin(WIFI_SSID, WIFI_PASS);
    while (WiFi.status() != WL_CONNECTED) {
        delay(500);
        digitalWrite(LED_PIN, !digitalRead(LED_PIN));
    }
    Serial.printf("WiFi connected: %s\n", WiFi.localIP().toString().c_str());

    // BLE
    NimBLEDevice::init("");

    // Scan and connect
    while (!tp.scan()) {
        Serial.println("Retrying scan in 5s...");
        delay(5000);
    }
    while (!tp.connect()) {
        Serial.println("Retrying connect in 5s...");
        delay(5000);
    }
    tp.sendHandshake();
    Serial.println("ThermoPro connected and streaming");
    digitalWrite(LED_PIN, HIGH);
}

void loop() {
    if (!tp.isConnected()) {
        Serial.println("Disconnected, reconnecting...");
        digitalWrite(LED_PIN, LOW);
        while (!tp.scan()) delay(5000);
        while (!tp.connect()) delay(5000);
        tp.sendHandshake();
        digitalWrite(LED_PIN, HIGH);
    }

    ProbeData data = tp.getLatestData();
    if (data.valid) {
        // POST to server
        HTTPClient http;
        String url = String(SERVER_URL) + "/api/data";
        http.begin(url);
        http.addHeader("Content-Type", "application/json");

        String json = "{\"probes\":[";
        for (int i = 0; i < NUM_PROBES; i++) {
            if (i > 0) json += ",";
            json += "{\"id\":" + String(i + 1) + ",\"temp_f\":" + String(data.temps[i], 1) + "}";
        }
        json += "],\"battery\":" + String(data.battery) + "}";

        int code = http.POST(json);
        if (code != 200) {
            Serial.printf("POST failed: %d\n", code);
        }
        http.end();
    }

    delay(3000);
}
```

**Step 4: Verify it compiles**

Run: `cd esp32 && pio run`
Expected: BUILD SUCCESS

**Step 5: Commit**

```bash
git add esp32/
git commit -m "feat: add BLE connection and temperature decoding for TP25"
```

---

## Task 13: ESP32 OTA Update Check

**Files:**
- Modify: `esp32/src/main.cpp`
- Create: `esp32/src/ota.h`
- Create: `esp32/src/ota.cpp`

**Step 1: Create ota.h and ota.cpp**

```cpp
// esp32/src/ota.h
#ifndef OTA_H
#define OTA_H

#include "config.h"

bool checkAndApplyOTA();

#endif
```

```cpp
// esp32/src/ota.cpp
#include "ota.h"
#include <HTTPClient.h>
#include <ArduinoJson.h>
#include <Update.h>

bool checkAndApplyOTA() {
    HTTPClient http;
    String url = String(SERVER_URL) + "/api/firmware/latest";
    http.begin(url);
    int code = http.GET();

    if (code != 200) {
        Serial.println("OTA: no firmware available");
        http.end();
        return false;
    }

    String body = http.getString();
    http.end();

    JsonDocument doc;
    deserializeJson(doc, body);
    int remoteVersion = doc["version"];

    if (remoteVersion <= FIRMWARE_VERSION) {
        Serial.printf("OTA: firmware up to date (v%d)\n", FIRMWARE_VERSION);
        return false;
    }

    Serial.printf("OTA: updating from v%d to v%d\n", FIRMWARE_VERSION, remoteVersion);

    // Download and flash
    HTTPClient dl;
    dl.begin(String(SERVER_URL) + "/api/firmware/download");
    int dlCode = dl.GET();
    if (dlCode != 200) {
        Serial.println("OTA: download failed");
        dl.end();
        return false;
    }

    int contentLength = dl.getSize();
    WiFiClient* stream = dl.getStreamPtr();

    if (!Update.begin(contentLength)) {
        Serial.println("OTA: not enough space");
        dl.end();
        return false;
    }

    Update.writeStream(*stream);
    if (Update.end()) {
        Serial.println("OTA: success, rebooting");
        dl.end();
        ESP.restart();
        return true;
    }

    Serial.printf("OTA: failed: %s\n", Update.errorString());
    dl.end();
    return false;
}
```

**Step 2: Add ArduinoJson dependency to platformio.ini**

```ini
lib_deps =
    h2zero/NimBLE-Arduino@^1.4.0
    bblanchon/ArduinoJson@^7.0.0
```

**Step 3: Call OTA check in setup() after WiFi connects, before BLE**

```cpp
// In setup(), after WiFi connected:
checkAndApplyOTA();
```

**Step 4: Verify it compiles**

Run: `cd esp32 && pio run`
Expected: BUILD SUCCESS

**Step 5: Commit**

```bash
git add esp32/
git commit -m "feat: add OTA firmware update check on boot"
```

---

## Task 14: Integration Testing and Smoke Test

**Step 1: Run all Go tests**

Run: `go test ./... -v`
Expected: All PASS

**Step 2: Build and start server**

Run: `make build && ./bin/therm-pro-server &`

**Step 3: Simulate ESP32 with curl**

```bash
# Post temperature data
curl -X POST http://localhost:8080/api/data \
  -H 'Content-Type: application/json' \
  -d '{"probes":[{"id":1,"temp_f":250},{"id":2,"temp_f":165},{"id":3,"temp_f":180},{"id":4,"temp_f":190}],"battery":85}'

# Check session
curl http://localhost:8080/api/session | jq .

# Set an alert
curl -X POST http://localhost:8080/api/alerts \
  -H 'Content-Type: application/json' \
  -d '{"probe_id":2,"alert":{"target_temp":203}}'

# Label probes
curl -X POST http://localhost:8080/api/alerts \
  -H 'Content-Type: application/json' \
  -d '{"probe_id":1,"alert":{"low_temp":225,"high_temp":275}}'

# Post data that triggers alert
curl -X POST http://localhost:8080/api/data \
  -H 'Content-Type: application/json' \
  -d '{"probes":[{"id":1,"temp_f":250},{"id":2,"temp_f":203},{"id":3,"temp_f":185},{"id":4,"temp_f":195}]}'

# Verify alert fired in server logs

# Reset session
curl -X POST http://localhost:8080/api/session/reset

# Check Swagger docs
curl http://localhost:8080/api/docs/index.html
```

**Step 4: Open dashboard in browser**

Open: `http://localhost:8080/`
Verify: probes display, chart renders, WebSocket connects

**Step 5: Commit any fixes**

```bash
git add -A
git commit -m "fix: integration test fixes"
```

---

## Task 15: Add .gitignore and Final Cleanup

**Files:**
- Create: `.gitignore`

**Step 1: Create .gitignore**

```
bin/
esp32/.pio/
esp32/.vscode/
*.bin
.DS_Store
```

**Step 2: Commit**

```bash
git add .gitignore
git commit -m "chore: add .gitignore"
```

---

Plan complete and saved to `docs/plans/2026-03-28-therm-pro-implementation.md`. Two execution options:

**1. Subagent-Driven (this session)** — I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** — Open a new session with executing-plans, batch execution with checkpoints

Which approach?