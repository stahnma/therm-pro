package auth

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// thermProUser implements webauthn.User for the single-user Therm-Pro app.
type thermProUser struct {
	credentials []webauthn.Credential
}

func (u *thermProUser) WebAuthnID() []byte                         { return []byte("therm-pro-user") }
func (u *thermProUser) WebAuthnName() string                       { return "therm-pro" }
func (u *thermProUser) WebAuthnDisplayName() string                { return "Therm-Pro User" }
func (u *thermProUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

const challengeTTL = 5 * time.Minute

// jsonError writes a JSON error response.
func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// WebAuthnHandler manages WebAuthn registration and login ceremonies.
// Single-user design: only one pending challenge at a time.
type WebAuthnHandler struct {
	wa              *webauthn.WebAuthn
	credStore       *CredentialStore
	sessionSecret   []byte
	registrationPIN string
	log             *slog.Logger

	mu               sync.Mutex
	pendingSession   *webauthn.SessionData
	pendingCreatedAt time.Time
}

// NewWebAuthnHandler creates a new WebAuthn handler.
// The RP ID (relying party identifier) is derived from the origin URL's hostname.
func NewWebAuthnHandler(rpName, rpOrigin, registrationPIN string, credStore *CredentialStore, dataDir string) (*WebAuthnHandler, error) {
	u, err := url.Parse(rpOrigin)
	if err != nil {
		return nil, err
	}
	rpID := u.Hostname()

	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName: rpName,
		RPID:          rpID,
		RPOrigins:     []string{rpOrigin},
	})
	if err != nil {
		return nil, err
	}

	secret, err := LoadOrCreateSessionSecret(dataDir)
	if err != nil {
		return nil, err
	}

	handler := &WebAuthnHandler{
		wa:              wa,
		credStore:       credStore,
		sessionSecret:   secret,
		registrationPIN: registrationPIN,
		log:             slog.Default().With("component", "webauthn"),
	}
	handler.log.Info("webauthn configured", "rp_id", rpID, "rp_origin", rpOrigin)
	return handler, nil
}

// user builds a thermProUser from the credential store.
func (h *WebAuthnHandler) user() *thermProUser {
	stored := h.credStore.Credentials()
	creds := make([]webauthn.Credential, len(stored))
	for i, sc := range stored {
		creds[i] = webauthn.Credential{
			ID:        sc.ID,
			PublicKey: sc.PublicKey,
			Flags: webauthn.CredentialFlags{
				BackupEligible: sc.BackupEligible,
			},
			Authenticator: webauthn.Authenticator{
				SignCount: sc.SignCount,
			},
		}
	}
	return &thermProUser{credentials: creds}
}

// pendingValid returns the pending session if it exists and has not expired.
// Must be called with mu held.
func (h *WebAuthnHandler) pendingValid() *webauthn.SessionData {
	if h.pendingSession == nil {
		return nil
	}
	if time.Since(h.pendingCreatedAt) > challengeTTL {
		h.pendingSession = nil
		return nil
	}
	return h.pendingSession
}

// checkRegistrationPIN validates the X-Registration-PIN header against the
// configured PIN. Returns true if the PIN is valid; otherwise it writes an
// error response and returns false.
func (h *WebAuthnHandler) checkRegistrationPIN(w http.ResponseWriter, r *http.Request) bool {
	if h.registrationPIN == "" {
		h.log.Warn("registration rejected: no registration PIN configured")
		jsonError(w, "registration not available", http.StatusForbidden)
		return false
	}
	pin := r.Header.Get("X-Registration-PIN")
	if subtle.ConstantTimeCompare([]byte(pin), []byte(h.registrationPIN)) != 1 {
		h.log.Warn("registration rejected: invalid PIN", "remote_addr", r.RemoteAddr)
		jsonError(w, "invalid registration PIN", http.StatusForbidden)
		return false
	}
	return true
}

// RegisterBegin starts the WebAuthn registration ceremony.
// PIN is not checked here — only on RegisterFinish, where the credential is
// actually stored. The challenge itself is not sensitive.
func (h *WebAuthnHandler) RegisterBegin(w http.ResponseWriter, r *http.Request) {
	h.log.Debug("register begin", "remote_addr", r.RemoteAddr)
	user := h.user()

	creation, session, err := h.wa.BeginRegistration(user)
	if err != nil {
		h.log.Error("begin registration failed", "error", err)
		jsonError(w, "registration failed", http.StatusInternalServerError)
		return
	}

	h.mu.Lock()
	h.pendingSession = session
	h.pendingCreatedAt = time.Now()
	h.mu.Unlock()

	h.log.Debug("register begin: challenge issued")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(creation)
}

// RegisterFinish completes the WebAuthn registration ceremony.
func (h *WebAuthnHandler) RegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !h.checkRegistrationPIN(w, r) {
		return
	}
	h.log.Debug("register finish", "remote_addr", r.RemoteAddr)
	user := h.user()

	h.mu.Lock()
	sessionData := h.pendingValid()
	h.pendingSession = nil
	h.mu.Unlock()

	if sessionData == nil {
		h.log.Warn("register finish rejected: no pending registration")
		jsonError(w, "no pending registration", http.StatusBadRequest)
		return
	}

	credential, err := h.wa.FinishRegistration(user, *sessionData, r)
	if err != nil {
		h.log.Error("registration verification failed", "error", err)
		jsonError(w, "registration verification failed", http.StatusBadRequest)
		return
	}

	h.credStore.Add(StoredCredential{
		ID:             credential.ID,
		PublicKey:      credential.PublicKey,
		BackupEligible: credential.Flags.BackupEligible,
		SignCount:      credential.Authenticator.SignCount,
		Label:          "Passkey",
	})
	if err := h.credStore.Save(); err != nil {
		h.log.Error("failed to save credential", "error", err)
		jsonError(w, "failed to save credential", http.StatusInternalServerError)
		return
	}

	h.log.Info("passkey registered", "remote_addr", r.RemoteAddr)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// LoginBegin starts the WebAuthn login ceremony.
func (h *WebAuthnHandler) LoginBegin(w http.ResponseWriter, r *http.Request) {
	h.log.Debug("login begin", "remote_addr", r.RemoteAddr, "user_agent", r.UserAgent())
	user := h.user()
	if len(user.credentials) == 0 {
		h.log.Warn("login begin rejected: no credentials registered")
		jsonError(w, "no credentials registered", http.StatusBadRequest)
		return
	}
	h.log.Debug("login begin: found credentials", "count", len(user.credentials))

	assertion, session, err := h.wa.BeginLogin(user)
	if err != nil {
		h.log.Error("begin login failed", "error", err)
		jsonError(w, "login failed", http.StatusInternalServerError)
		return
	}

	h.mu.Lock()
	h.pendingSession = session
	h.pendingCreatedAt = time.Now()
	h.mu.Unlock()

	h.log.Debug("login begin: challenge issued")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(assertion)
}

// LoginFinish completes the WebAuthn login ceremony and sets a session cookie.
func (h *WebAuthnHandler) LoginFinish(w http.ResponseWriter, r *http.Request) {
	h.log.Debug("login finish", "remote_addr", r.RemoteAddr)
	user := h.user()

	h.mu.Lock()
	sessionData := h.pendingValid()
	h.pendingSession = nil
	h.mu.Unlock()

	if sessionData == nil {
		h.log.Warn("login finish rejected: no pending session (expired or missing)")
		jsonError(w, "no pending login", http.StatusBadRequest)
		return
	}

	credential, err := h.wa.FinishLogin(user, *sessionData, r)
	if err != nil {
		h.log.Error("login verification failed", "error", err)
		jsonError(w, "login verification failed", http.StatusBadRequest)
		return
	}

	h.credStore.UpdateSignCount(credential.ID, credential.Authenticator.SignCount)
	if err := h.credStore.Save(); err != nil {
		h.log.Error("failed to update sign count", "error", err)
	}

	SetSessionCookie(w, h.sessionSecret)
	h.log.Info("login succeeded", "remote_addr", r.RemoteAddr)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ValidateSession checks whether the request carries a valid session cookie.
// This satisfies the SessionValidator type.
func (h *WebAuthnHandler) ValidateSession(r *http.Request) bool {
	return ValidateSessionCookie(r, h.sessionSecret)
}
