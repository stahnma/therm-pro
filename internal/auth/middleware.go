package auth

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// SessionValidator checks whether the current request carries a valid session.
type SessionValidator func(r *http.Request) bool

// RequireAuth returns middleware that blocks requests not carrying a valid session.
func RequireAuth(validateSession SessionValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if validateSession != nil && validateSession(r) {
				next.ServeHTTP(w, r)
				return
			}
			slog.Info("access denied: unauthorized", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		})
	}
}
