package cook

import (
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
