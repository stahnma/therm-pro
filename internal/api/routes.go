// internal/api/routes.go
package api

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"time"

	"github.com/stahnma/therm-pro/internal/slack"
	"github.com/stahnma/therm-pro/internal/web"
)

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
	mux.HandleFunc("GET /api/docs", func(w http.ResponseWriter, r *http.Request) {
		staticFS, _ := fs.Sub(web.StaticFiles, "static")
		f, err := staticFS.Open("docs.html")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "docs.html", time.Time{}, f.(io.ReadSeeker))
	})
	mux.HandleFunc("GET /diagnostics", s.handleDiagnostics)
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"commit": s.gitCommit})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	})

	// Slack slash command
	if s.slackSigningSecret != "" {
		cmdHandler := slack.NewCommandHandler(s.slackSigningSecret, s.slackBotToken, s.session)
		mux.Handle("POST /slack/command", cmdHandler)
	}

	// Serve embedded web dashboard
	staticFS, _ := fs.Sub(web.StaticFiles, "static")
	mux.Handle("GET /", http.FileServer(http.FS(staticFS)))

	return mux
}
