// internal/api/routes.go
package api

import (
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/stahnma/therm-pro/internal/auth"
	"github.com/stahnma/therm-pro/internal/slack"
	"github.com/stahnma/therm-pro/internal/web"
)

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Set up WebAuthn passkey authentication.
	credStore := auth.NewCredentialStore(filepath.Join(s.config.DataDir, "passkeys.json"))
	if err := credStore.Load(); err != nil {
		log.Printf("WARNING: failed to load credential store: %v", err)
	}

	var sessionValidator auth.SessionValidator
	var webauthnHandler *auth.WebAuthnHandler

	wh, err := auth.NewWebAuthnHandler(
		"Therm-Pro",
		s.config.WebAuthnOrigin,
		credStore,
		s.config.DataDir,
	)
	if err != nil {
		log.Printf("WARNING: WebAuthn setup failed: %v", err)
	} else {
		webauthnHandler = wh
		sessionValidator = wh.ValidateSession
	}

	requireAuth := auth.RequireAuth(s.config.AllowedCIDR, s.config.TrustProxy, sessionValidator)
	requireHome := auth.RequireHomeNetwork(s.config.AllowedCIDR, s.config.TrustProxy)

	mux.HandleFunc("POST /api/data", s.handlePostData)
	mux.HandleFunc("GET /api/session", s.handleGetSession)
	mux.Handle("POST /api/session/reset", requireAuth(http.HandlerFunc(s.handleResetSession)))
	mux.Handle("POST /api/alerts", requireAuth(http.HandlerFunc(s.handlePostAlerts)))
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
	mux.HandleFunc("GET /api/firmware/latest", s.firmware.HandleLatest)
	mux.HandleFunc("GET /api/firmware/download", s.firmware.HandleDownload)
	mux.Handle("POST /api/firmware/upload", requireAuth(http.HandlerFunc(s.firmware.HandleUpload)))
	mux.HandleFunc("GET /api/auth/status", auth.StatusHandler(s.config.AllowedCIDR, s.config.TrustProxy, sessionValidator))
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

	// WebAuthn passkey routes
	if webauthnHandler != nil {
		mux.Handle("POST /auth/register/begin", requireHome(http.HandlerFunc(webauthnHandler.RegisterBegin)))
		mux.Handle("POST /auth/register/finish", requireHome(http.HandlerFunc(webauthnHandler.RegisterFinish)))
		mux.HandleFunc("POST /auth/login/begin", webauthnHandler.LoginBegin)
		mux.HandleFunc("POST /auth/login/finish", webauthnHandler.LoginFinish)
	}

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
