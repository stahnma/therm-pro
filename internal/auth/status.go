package auth

import (
	"encoding/json"
	"net/http"
)

type StatusResponse struct {
	Role        string `json:"role"`
	CanRegister bool   `json:"can_register"`
}

// StatusHandler returns an http.HandlerFunc that reports the caller's access level.
func StatusHandler(cidr string, trustProxy bool, validateSession SessionValidator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		isHome := IsHomeNetwork(r, cidr, trustProxy)
		isAuthed := validateSession != nil && validateSession(r)

		resp := StatusResponse{
			Role:        "viewer",
			CanRegister: isHome,
		}
		if isHome || isAuthed {
			resp.Role = "admin"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}
