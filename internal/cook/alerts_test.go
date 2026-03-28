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
	_ = e.Evaluate(probe)  // first time fires
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
