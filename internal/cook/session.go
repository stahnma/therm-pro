package cook

import (
	"fmt"
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
	Timestamp time.Time  `json:"timestamp"`
	Temps     [4]float64 `json:"temps"`
	Battery   int        `json:"battery"`
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

func (s *Session) RLock()   { s.mu.RLock() }
func (s *Session) RUnlock() { s.mu.RUnlock() }

func (s *Session) SetAlert(probeID int, alert AlertConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Probes[probeID-1].Alert = alert
}
