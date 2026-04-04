package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type StatusResponse struct {
	Role        string `json:"role"`
	CanRegister bool   `json:"can_register"`
}

// StatusHandler returns an http.HandlerFunc that reports the caller's access level.
func StatusHandler(validateSession SessionValidator, registrationPIN string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		isAuthed := validateSession != nil && validateSession(r)

		resp := StatusResponse{
			Role:        "viewer",
			CanRegister: registrationPIN != "",
		}
		if isAuthed {
			resp.Role = "admin"
		}

		slog.Debug("auth status", "role", resp.Role, "can_register", resp.CanRegister, "is_authed", isAuthed, "remote_addr", r.RemoteAddr)

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.Error("failed to write auth status response", "error", err)
		}
	}
}
