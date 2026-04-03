package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSessionCookie_RoundTrip(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-enough")

	rec := httptest.NewRecorder()
	SetSessionCookie(rec, secret)

	// Extract cookie from response and put it in a request.
	resp := rec.Result()
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie to be set")
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(cookies[0])

	if !ValidateSessionCookie(req, secret) {
		t.Fatal("expected session cookie to be valid")
	}
}

func TestSessionCookie_WrongSecret(t *testing.T) {
	secret1 := []byte("secret-one-32-bytes-long-enough!")
	secret2 := []byte("secret-two-32-bytes-long-enough!")

	rec := httptest.NewRecorder()
	SetSessionCookie(rec, secret1)

	resp := rec.Result()
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(resp.Cookies()[0])

	if ValidateSessionCookie(req, secret2) {
		t.Fatal("expected session cookie to be invalid with wrong secret")
	}
}

func TestSessionCookie_Expired(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-enough")

	// Craft an expired cookie value manually.
	expiryStr := "1000000000" // well in the past (2001)
	sig := computeHMAC(expiryStr, secret)

	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: expiryStr + "." + sig})
	if ValidateSessionCookie(req, secret) {
		t.Fatal("expected expired cookie to be invalid")
	}
}

func TestSessionCookie_NoCookie(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-enough")
	req := httptest.NewRequest("GET", "/", nil)
	if ValidateSessionCookie(req, secret) {
		t.Fatal("expected missing cookie to be invalid")
	}
}

func TestSessionCookie_MalformedValue(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-enough")
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-a-valid-cookie"})
	if ValidateSessionCookie(req, secret) {
		t.Fatal("expected malformed cookie to be invalid")
	}
}

func TestWebAuthnHandler_Creation(t *testing.T) {
	dir := t.TempDir()
	credStore := NewCredentialStore(filepath.Join(dir, "passkeys.json"))

	handler, err := NewWebAuthnHandler("Therm-Pro", "http://localhost:8088", credStore, dir)
	if err != nil {
		t.Fatalf("failed to create WebAuthn handler: %v", err)
	}
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestWebAuthnHandler_ValidateSessionIntegration(t *testing.T) {
	dir := t.TempDir()
	credStore := NewCredentialStore(filepath.Join(dir, "passkeys.json"))

	handler, err := NewWebAuthnHandler("Therm-Pro", "http://localhost:8088", credStore, dir)
	if err != nil {
		t.Fatalf("failed to create WebAuthn handler: %v", err)
	}

	// No cookie — should be invalid.
	req := httptest.NewRequest("GET", "/", nil)
	if handler.ValidateSession(req) {
		t.Fatal("expected no session to be invalid")
	}

	// Set a cookie and validate.
	rec := httptest.NewRecorder()
	SetSessionCookie(rec, handler.sessionSecret)
	resp := rec.Result()

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.AddCookie(resp.Cookies()[0])
	if !handler.ValidateSession(req2) {
		t.Fatal("expected valid session")
	}
}

func TestLoadOrCreateSessionSecret(t *testing.T) {
	dir := t.TempDir()

	secret1, err := LoadOrCreateSessionSecret(dir)
	if err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if len(secret1) != 32 {
		t.Fatalf("expected 32 byte secret, got %d", len(secret1))
	}

	// Second call should return the same secret.
	secret2, err := LoadOrCreateSessionSecret(dir)
	if err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if string(secret1) != string(secret2) {
		t.Fatal("expected same secret on reload")
	}
}
