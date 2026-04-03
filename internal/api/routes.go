// internal/api/routes.go
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
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
		slog.Warn("failed to load credential store", "error", err)
	}

	var sessionValidator auth.SessionValidator
	var webauthnHandler *auth.WebAuthnHandler

	wh, err := auth.NewWebAuthnHandler(
		"Therm-Pro",
		s.config.WebAuthnOrigin,
		s.config.RegistrationPIN,
		credStore,
		s.config.DataDir,
	)
	if err != nil {
		slog.Warn("webauthn setup failed", "error", err)
	} else {
		webauthnHandler = wh
		slog.Info("webauthn configured", "origin", s.config.WebAuthnOrigin)
		sessionValidator = wh.ValidateSession
	}

	requireAuth := auth.RequireAuth(sessionValidator)

	mux.HandleFunc("POST /api/data", s.handlePostData)
	mux.HandleFunc("GET /api/session", s.handleGetSession)
	mux.Handle("POST /api/session/reset", requireAuth(http.HandlerFunc(s.handleResetSession)))
	mux.Handle("POST /api/alerts", requireAuth(http.HandlerFunc(s.handlePostAlerts)))
	mux.HandleFunc("GET /api/ws", s.handleWebSocket)
	mux.HandleFunc("GET /api/firmware/latest", s.firmware.HandleLatest)
	mux.HandleFunc("GET /api/firmware/download", s.firmware.HandleDownload)
	mux.Handle("POST /api/firmware/upload", requireAuth(http.HandlerFunc(s.firmware.HandleUpload)))
	mux.HandleFunc("GET /api/auth/status", auth.StatusHandler(sessionValidator, s.config.RegistrationPIN))
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

	// Temporary debug endpoint — captures client-side errors to a file
	mux.HandleFunc("POST /api/debug/client-error", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		entry := fmt.Sprintf("[%s] %s\n", time.Now().Format(time.RFC3339), string(body))
		f, err := os.OpenFile(filepath.Join(s.config.DataDir, "client-debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err == nil {
			f.WriteString(entry)
			f.Close()
		}
		slog.Info("client debug", "payload", string(body))
		w.WriteHeader(http.StatusNoContent)
	})

	// WebAuthn passkey routes
	if webauthnHandler != nil {
		mux.HandleFunc("POST /auth/register/begin", webauthnHandler.RegisterBegin)
		mux.HandleFunc("POST /auth/register/finish", webauthnHandler.RegisterFinish)
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
