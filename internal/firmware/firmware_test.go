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
