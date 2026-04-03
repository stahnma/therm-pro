package auth

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// SessionValidator checks whether the current request carries a valid session.
type SessionValidator func(r *http.Request) bool

// IsHomeNetwork returns true when the request originates from the given CIDR.
// When trustProxy is true the first address in X-Forwarded-For is used instead
// of RemoteAddr.
func IsHomeNetwork(r *http.Request, cidr string, trustProxy bool) bool {
	_, subnet, err := net.ParseCIDR(cidr)
	if err != nil {
		slog.Warn("invalid CIDR config", "cidr", cidr, "error", err)
		return false
	}

	ipStr := r.RemoteAddr
	ipSource := "RemoteAddr"
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ipStr = strings.TrimSpace(strings.Split(xff, ",")[0])
			ipSource = "X-Forwarded-For"
		}
	}

	host, _, err := net.SplitHostPort(ipStr)
	if err != nil {
		host = ipStr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		slog.Warn("failed to parse IP", "ip_str", ipStr, "source", ipSource)
		return false
	}
	result := subnet.Contains(ip)
	slog.Debug("home network check", "ip", host, "source", ipSource, "cidr", cidr, "result", result)
	return result
}

// RequireHomeNetwork returns middleware that blocks requests not originating
// from the configured home network CIDR. Session cookies are not accepted.
// Returns 403 Forbidden (not 401) since this is about location, not authentication.
func RequireHomeNetwork(cidr string, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsHomeNetwork(r, cidr, trustProxy) {
				next.ServeHTTP(w, r)
				return
			}
			slog.Info("access denied: not home network", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
		})
	}
}

// RequireAuth returns middleware that blocks requests not from the home network
// and not carrying a valid session.
func RequireAuth(cidr string, trustProxy bool, validateSession SessionValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsHomeNetwork(r, cidr, trustProxy) {
				next.ServeHTTP(w, r)
				return
			}
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
