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
		minInterval: 0,
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
		return fmt.Sprintf("%s (Probe %d) hit target: %.1f\u00b0F", p.Label, p.ID, p.CurrentTemp)
	case AlertHighTemp:
		return fmt.Sprintf("%s (Probe %d) above %.1f\u00b0F: currently %.1f\u00b0F", p.Label, p.ID, threshold, p.CurrentTemp)
	case AlertLowTemp:
		return fmt.Sprintf("%s (Probe %d) below %.1f\u00b0F: currently %.1f\u00b0F", p.Label, p.ID, threshold, p.CurrentTemp)
	default:
		return fmt.Sprintf("%s (Probe %d): %.1f\u00b0F", p.Label, p.ID, p.CurrentTemp)
	}
}
