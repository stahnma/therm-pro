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
