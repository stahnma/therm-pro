package auth

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

// thermProUser implements webauthn.User for the single-user Therm-Pro app.
type thermProUser struct {
	credentials []webauthn.Credential
}

func (u *thermProUser) WebAuthnID() []byte          { return []byte("therm-pro-user") }
func (u *thermProUser) WebAuthnName() string         { return "therm-pro" }
func (u *thermProUser) WebAuthnDisplayName() string  { return "Therm-Pro User" }
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
	wa            *webauthn.WebAuthn
	credStore     *CredentialStore
	sessionSecret []byte

	mu               sync.Mutex
	pendingSession   *webauthn.SessionData
	pendingCreatedAt time.Time
}

// NewWebAuthnHandler creates a new WebAuthn handler.
func NewWebAuthnHandler(rpName, rpID, rpOrigin string, credStore *CredentialStore, dataDir string) (*WebAuthnHandler, error) {
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

	return &WebAuthnHandler{
		wa:            wa,
		credStore:     credStore,
		sessionSecret: secret,
	}, nil
}

// user builds a thermProUser from the credential store.
func (h *WebAuthnHandler) user() *thermProUser {
	stored := h.credStore.Credentials()
	creds := make([]webauthn.Credential, len(stored))
	for i, sc := range stored {
		creds[i] = webauthn.Credential{
			ID:        sc.ID,
			PublicKey: sc.PublicKey,
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

// RegisterBegin starts the WebAuthn registration ceremony.
func (h *WebAuthnHandler) RegisterBegin(w http.ResponseWriter, r *http.Request) {
	user := h.user()

	creation, session, err := h.wa.BeginRegistration(user)
	if err != nil {
		log.Printf("webauthn: begin registration error: %v", err)
		jsonError(w, "registration failed", http.StatusInternalServerError)
		return
	}

	h.mu.Lock()
	h.pendingSession = session
	h.pendingCreatedAt = time.Now()
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(creation)
}

// RegisterFinish completes the WebAuthn registration ceremony.
func (h *WebAuthnHandler) RegisterFinish(w http.ResponseWriter, r *http.Request) {
	user := h.user()

	h.mu.Lock()
	sessionData := h.pendingValid()
	h.mu.Unlock()

	if sessionData == nil {
		jsonError(w, "no pending registration", http.StatusBadRequest)
		return
	}

	credential, err := h.wa.FinishRegistration(user, *sessionData, r)
	if err != nil {
		log.Printf("webauthn: finish registration error: %v", err)
		jsonError(w, "registration verification failed", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	h.pendingSession = nil
	h.mu.Unlock()

	h.credStore.Add(StoredCredential{
		ID:        credential.ID,
		PublicKey: credential.PublicKey,
		Label:     "Passkey",
	})
	if err := h.credStore.Save(); err != nil {
		log.Printf("webauthn: save credential error: %v", err)
		jsonError(w, "failed to save credential", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// LoginBegin starts the WebAuthn login ceremony.
func (h *WebAuthnHandler) LoginBegin(w http.ResponseWriter, r *http.Request) {
	user := h.user()
	if len(user.credentials) == 0 {
		jsonError(w, "no credentials registered", http.StatusBadRequest)
		return
	}

	assertion, session, err := h.wa.BeginLogin(user)
	if err != nil {
		log.Printf("webauthn: begin login error: %v", err)
		jsonError(w, "login failed", http.StatusInternalServerError)
		return
	}

	h.mu.Lock()
	h.pendingSession = session
	h.pendingCreatedAt = time.Now()
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(assertion)
}

// LoginFinish completes the WebAuthn login ceremony and sets a session cookie.
func (h *WebAuthnHandler) LoginFinish(w http.ResponseWriter, r *http.Request) {
	user := h.user()

	h.mu.Lock()
	sessionData := h.pendingValid()
	h.mu.Unlock()

	if sessionData == nil {
		jsonError(w, "no pending login", http.StatusBadRequest)
		return
	}

	_, err := h.wa.FinishLogin(user, *sessionData, r)
	if err != nil {
		log.Printf("webauthn: finish login error: %v", err)
		jsonError(w, "login verification failed", http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	h.pendingSession = nil
	h.mu.Unlock()

	SetSessionCookie(w, h.sessionSecret)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ValidateSession checks whether the request carries a valid session cookie.
// This satisfies the SessionValidator type.
func (h *WebAuthnHandler) ValidateSession(r *http.Request) bool {
	return ValidateSessionCookie(r, h.sessionSecret)
}
