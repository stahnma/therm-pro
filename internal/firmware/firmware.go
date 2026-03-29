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

// Version returns the current firmware version number.
func (s *Store) Version() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.version
}

func (s *Store) HandleDownload(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	binPath := filepath.Join(s.dir, "firmware.bin")
	http.ServeFile(w, r, binPath)
}
