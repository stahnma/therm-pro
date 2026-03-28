package api

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/stahnma/therm-pro/internal/cook"
	"github.com/stahnma/therm-pro/internal/firmware"
	"github.com/stahnma/therm-pro/internal/slack"
)

// Server holds all dependencies for the HTTP API.
type Server struct {
	addr        string
	session     *cook.Session
	alerts      *cook.AlertEngine
	slack       *slack.Client
	firmware    *firmware.Store
	sessionPath string
	wsClients   map[*wsClient]bool
	wsMu        sync.Mutex
}

// ProbeReading represents a single probe temperature reading from the device.
type ProbeReading struct {
	ID    int     `json:"id"`
	TempF float64 `json:"temp_f"`
}

// DataPayload is the JSON body for POST /api/data.
type DataPayload struct {
	Probes    []ProbeReading `json:"probes"`
	Timestamp string         `json:"timestamp,omitempty"`
	Battery   int            `json:"battery,omitempty"`
}

// AlertPayload is the JSON body for POST /api/alerts.
type AlertPayload struct {
	ProbeID int              `json:"probe_id"`
	Alert   cook.AlertConfig `json:"alert"`
}

// NewServer creates a new Server with the given address, Slack webhook URL,
// and session persistence path.
func NewServer(addr, slackWebhook, sessionPath, firmwareDir string) *Server {
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
		firmware:    firmware.NewStore(firmwareDir),
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

	// Persist session to disk.
	if s.sessionPath != "" {
		if err := cook.Save(s.session, s.sessionPath); err != nil {
			log.Printf("warning: could not save session: %v", err)
		}
	}

	// Evaluate alerts for each probe.
	s.session.RLock()
	for i := 0; i < cook.NumProbes; i++ {
		fired := s.alerts.Evaluate(s.session.Probes[i])
		for _, alert := range fired {
			log.Printf("ALERT: %s", alert.Message)
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

