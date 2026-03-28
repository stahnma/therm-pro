// internal/api/routes.go
package api

import "net/http"

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/data", s.handlePostData)
	mux.HandleFunc("GET /api/session", s.handleGetSession)
	mux.HandleFunc("POST /api/session/reset", s.handleResetSession)
	mux.HandleFunc("POST /api/alerts", s.handlePostAlerts)
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
	mux.HandleFunc("GET /api/firmware/latest", s.firmware.HandleLatest)
	mux.HandleFunc("GET /api/firmware/download", s.firmware.HandleDownload)
	mux.HandleFunc("POST /api/firmware/upload", s.firmware.HandleUpload)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	})
	return mux
}
