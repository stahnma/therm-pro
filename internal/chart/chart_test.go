package chart

import (
	"bytes"
	"image/png"
	"testing"
	"time"

	"github.com/stahnma/therm-pro/internal/cook"
)

func TestRenderSessionChart_ValidPNG(t *testing.T) {
	start := time.Now().Add(-2 * time.Hour)
	history := make([]cook.Reading, 0, 24)
	for i := 0; i < 24; i++ {
		history = append(history, cook.Reading{
			Timestamp: start.Add(time.Duration(i) * 5 * time.Minute),
			Temps:     [4]float64{275.0 + float64(i)*0.5, 150.0 + float64(i)*2.0, cook.ProbeDisconnected, cook.ProbeDisconnected},
			Battery:   85,
		})
	}

	probes := [4]cook.Probe{
		{ID: 1, Label: "Pit", CurrentTemp: 286.5, Connected: true},
		{ID: 2, Label: "Brisket", CurrentTemp: 196.0, Connected: true},
		{ID: 3, Label: "Probe 3", Connected: false},
		{ID: 4, Label: "Probe 4", Connected: false},
	}

	data, err := RenderSessionChart(history, probes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty PNG data")
	}

	// Verify it's a valid PNG
	_, err = png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("output is not valid PNG: %v", err)
	}
}

func TestRenderSessionChart_NoConnectedProbes(t *testing.T) {
	history := []cook.Reading{
		{Timestamp: time.Now(), Temps: [4]float64{cook.ProbeDisconnected, cook.ProbeDisconnected, cook.ProbeDisconnected, cook.ProbeDisconnected}, Battery: 90},
	}
	probes := [4]cook.Probe{
		{ID: 1, Label: "Probe 1", Connected: false},
		{ID: 2, Label: "Probe 2", Connected: false},
		{ID: 3, Label: "Probe 3", Connected: false},
		{ID: 4, Label: "Probe 4", Connected: false},
	}

	data, err := RenderSessionChart(history, probes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty PNG even with no connected probes")
	}
}

func TestRenderSessionChart_EmptyHistory(t *testing.T) {
	probes := [4]cook.Probe{}
	data, err := RenderSessionChart(nil, probes)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty PNG even with empty history")
	}
}
