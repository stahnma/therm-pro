package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/stahnma/therm-pro/internal/config"
	"github.com/stahnma/therm-pro/internal/consul"
	"github.com/stahnma/therm-pro/internal/cook"
	"github.com/stahnma/therm-pro/internal/firmware"
	"github.com/stahnma/therm-pro/internal/slack"
)

// ClientStatus tracks the most recent state reported by the esp32 client.
type ClientStatus struct {
	mu              sync.RWMutex
	LastSeen        time.Time `json:"last_seen"`
	IP              string    `json:"ip"`
	FirmwareVersion int       `json:"firmware_version"`
	BLEConnected    bool      `json:"ble_connected"`
}

// Server holds all dependencies for the HTTP API.
type Server struct {
	addr               string
	session            *cook.Session
	alerts             *cook.AlertEngine
	slack              *slack.Client
	slackSigningSecret string
	slackBotToken      string
	firmware           *firmware.Store
	sessionPath        string
	gitCommit          string
	wsClients          map[*wsClient]bool
	wsMu               sync.Mutex
	clientStatus       ClientStatus
	config             *config.Config
}

// ProbeReading represents a single probe temperature reading from the device.
type ProbeReading struct {
	ID    int     `json:"id"`
	TempF float64 `json:"temp_f"`
}

// DataPayload is the JSON body for POST /api/data.
type DataPayload struct {
	Probes          []ProbeReading `json:"probes"`
	Timestamp       string         `json:"timestamp,omitempty"`
	Battery         int            `json:"battery,omitempty"`
	FirmwareVersion int            `json:"firmware_version,omitempty"`
	BLEConnected    *bool          `json:"ble_connected,omitempty"`
}

// AlertPayload is the JSON body for POST /api/alerts.
type AlertPayload struct {
	ProbeID int              `json:"probe_id"`
	Alert   cook.AlertConfig `json:"alert"`
}

// NewServer creates a new Server from the given configuration.
func NewServer(cfg *config.Config, gitCommit string) *Server {
	sessionPath := filepath.Join(cfg.DataDir, "session.json")
	firmwareDir := filepath.Join(cfg.DataDir, "firmware")

	session, err := cook.Load(sessionPath)
	if err != nil {
		slog.Warn("could not load session", "error", err)
		session = cook.NewSession()
	}
	return &Server{
		addr:               ":" + strconv.Itoa(cfg.Port),
		session:            session,
		alerts:             cook.NewAlertEngine(),
		slack:              slack.NewClient(cfg.Slack.Webhook),
		slackSigningSecret: cfg.Slack.SigningSecret,
		slackBotToken:      cfg.Slack.BotToken,
		firmware:           firmware.NewStore(firmwareDir),
		sessionPath:        sessionPath,
		gitCommit:          gitCommit,
		wsClients:          make(map[*wsClient]bool),
		config:             cfg,
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

	// Track esp32 client status.
	s.clientStatus.mu.Lock()
	s.clientStatus.LastSeen = time.Now()
	s.clientStatus.IP = r.RemoteAddr
	if payload.FirmwareVersion > 0 {
		s.clientStatus.FirmwareVersion = payload.FirmwareVersion
	}
	if payload.BLEConnected != nil {
		s.clientStatus.BLEConnected = *payload.BLEConnected
	}
	s.clientStatus.mu.Unlock()

	// Persist session to disk.
	if s.sessionPath != "" {
		if err := cook.Save(s.session, s.sessionPath); err != nil {
			slog.Warn("could not save session", "error", err)
		}
	}

	// Evaluate alerts for each probe.
	s.session.RLock()
	for i := 0; i < cook.NumProbes; i++ {
		fired := s.alerts.Evaluate(s.session.Probes[i])
		for _, alert := range fired {
			slog.Info("alert fired", "message", alert.Message)
			go s.slack.SendAlert(alert, temps)
		}
	}
	s.session.RUnlock()

	// Broadcast to WebSocket clients.
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

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	type espStatus struct {
		Status          string `json:"status"`
		IP              string `json:"ip,omitempty"`
		FirmwareVersion int    `json:"firmware_version,omitempty"`
		BLEConnected    bool   `json:"ble_connected"`
		LastSeen        string `json:"last_seen,omitempty"`
		DataAge         string `json:"data_age,omitempty"`
		DataAgeSec      int    `json:"data_age_seconds,omitempty"`
	}

	type diagnostics struct {
		Status          string        `json:"status"`
		ServerFirmware  int           `json:"server_firmware_version"`
		Consul          consul.Status `json:"consul"`
		ESP32           espStatus     `json:"esp32"`
	}

	// Overall status starts as ok.
	overall := "ok"

	// Consul status.
	consulStatus := consul.GetStatus()
	if !consulStatus.Healthy {
		overall = "degraded"
	}

	// ESP32 client status.
	s.clientStatus.mu.RLock()
	esp := espStatus{
		FirmwareVersion: s.clientStatus.FirmwareVersion,
		BLEConnected:    s.clientStatus.BLEConnected,
		IP:              s.clientStatus.IP,
	}
	lastSeen := s.clientStatus.LastSeen
	s.clientStatus.mu.RUnlock()

	if lastSeen.IsZero() {
		esp.Status = "no data received"
		overall = "degraded"
	} else {
		age := time.Since(lastSeen)
		esp.LastSeen = lastSeen.Format(time.RFC3339)
		esp.DataAge = age.Round(time.Second).String()
		esp.DataAgeSec = int(age.Seconds())
		if age > 30*time.Second {
			esp.Status = "stale"
			overall = "degraded"
		} else {
			esp.Status = "ok"
		}
		if !esp.BLEConnected {
			esp.Status = "ble_disconnected"
			overall = "degraded"
		}
	}

	diag := diagnostics{
		Status:         overall,
		ServerFirmware: s.firmware.Version(),
		Consul:         consulStatus,
		ESP32:          esp,
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(diag)
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

