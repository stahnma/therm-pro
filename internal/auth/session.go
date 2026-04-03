package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	sessionCookieName = "therm_pro_session"
	sessionTTL        = 24 * time.Hour
)

// LoadOrCreateSessionSecret reads the session secret from dataDir/session_secret,
// creating one with 32 random bytes if it does not exist.
func LoadOrCreateSessionSecret(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, "session_secret")
	data, err := os.ReadFile(path)
	if err == nil {
		decoded, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err == nil && len(decoded) == 32 {
			return decoded, nil
		}
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generating session secret: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(secret)+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("writing session secret: %w", err)
	}

	return secret, nil
}

// SetSessionCookie sets an HMAC-signed session cookie on the response.
func SetSessionCookie(w http.ResponseWriter, secret []byte) {
	expiry := time.Now().Add(sessionTTL).Unix()
	expiryStr := strconv.FormatInt(expiry, 10)
	sig := computeHMAC(expiryStr, secret)

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    expiryStr + "." + sig,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

// ValidateSessionCookie checks whether the request carries a valid, non-expired
// HMAC-signed session cookie.
func ValidateSessionCookie(r *http.Request, secret []byte) bool {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}

	parts := strings.SplitN(cookie.Value, ".", 2)
	if len(parts) != 2 {
		return false
	}

	expiryStr, sig := parts[0], parts[1]

	// Verify HMAC
	expected := computeHMAC(expiryStr, secret)
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return false
	}

	// Check expiry
	expiry, err := strconv.ParseInt(expiryStr, 10, 64)
	if err != nil {
		return false
	}
	return time.Now().Unix() < expiry
}

func computeHMAC(message string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
