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
func StatusHandler(validateSession SessionValidator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		isAuthed := validateSession != nil && validateSession(r)

		resp := StatusResponse{
			Role:        "viewer",
			CanRegister: false,
		}
		if isAuthed {
			resp.Role = "admin"
		}

		slog.Debug("auth status", "role", resp.Role, "is_authed", isAuthed, "remote_addr", r.RemoteAddr)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
