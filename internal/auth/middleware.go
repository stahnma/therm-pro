package auth

import (
	"net"
	"net/http"
	"strings"
)

// IsHomeNetwork checks if the request originates from the allowed CIDR range.
func IsHomeNetwork(r *http.Request, cidr string, trustProxy bool) bool {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}

	var ipStr string
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ipStr = strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}

	if ipStr == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return false
		}
		ipStr = host
	}

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return network.Contains(ip)
}

// SessionValidator is a function that checks if a request has a valid session cookie.
type SessionValidator func(r *http.Request) bool

// RequireAuth returns middleware that allows requests from the home network
// or with a valid session cookie. Returns 401 otherwise.
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
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})
	}
}

// RequireHomeNetwork returns middleware that only allows requests from the home network.
func RequireHomeNetwork(cidr string, trustProxy bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if IsHomeNetwork(r, cidr, trustProxy) {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "forbidden", http.StatusForbidden)
		})
	}
}
