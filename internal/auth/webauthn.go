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

// challengeEntry holds in-flight registration/login session data with a TTL.
type challengeEntry struct {
	session   *webauthn.SessionData
	createdAt time.Time
}

const challengeTTL = 5 * time.Minute

// WebAuthnHandler manages WebAuthn registration and login ceremonies.
type WebAuthnHandler struct {
	wa            *webauthn.WebAuthn
	credStore     *CredentialStore
	sessionSecret []byte

	mu         sync.Mutex
	challenges map[string]challengeEntry // keyed by challenge string
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
		challenges:    make(map[string]challengeEntry),
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

// cleanExpiredChallenges removes old challenge entries. Must be called with mu held.
func (h *WebAuthnHandler) cleanExpiredChallenges() {
	now := time.Now()
	for k, v := range h.challenges {
		if now.Sub(v.createdAt) > challengeTTL {
			delete(h.challenges, k)
		}
	}
}

// RegisterBegin starts the WebAuthn registration ceremony.
func (h *WebAuthnHandler) RegisterBegin(w http.ResponseWriter, r *http.Request) {
	user := h.user()

	creation, session, err := h.wa.BeginRegistration(user)
	if err != nil {
		log.Printf("webauthn: begin registration error: %v", err)
		http.Error(w, `{"error":"registration failed"}`, http.StatusInternalServerError)
		return
	}

	h.mu.Lock()
	h.cleanExpiredChallenges()
	h.challenges[session.Challenge] = challengeEntry{
		session:   session,
		createdAt: time.Now(),
	}
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(creation)
}

// RegisterFinish completes the WebAuthn registration ceremony.
func (h *WebAuthnHandler) RegisterFinish(w http.ResponseWriter, r *http.Request) {
	user := h.user()

	// Find the session — we need to try all pending registration challenges.
	h.mu.Lock()
	h.cleanExpiredChallenges()
	var matchedKey string
	var sessionData *webauthn.SessionData
	for k, v := range h.challenges {
		matchedKey = k
		sessionData = v.session
		break // single-user: use the most recent challenge
	}
	h.mu.Unlock()

	if sessionData == nil {
		http.Error(w, `{"error":"no pending registration"}`, http.StatusBadRequest)
		return
	}

	credential, err := h.wa.FinishRegistration(user, *sessionData, r)
	if err != nil {
		log.Printf("webauthn: finish registration error: %v", err)
		http.Error(w, `{"error":"registration verification failed"}`, http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	delete(h.challenges, matchedKey)
	h.mu.Unlock()

	h.credStore.Add(StoredCredential{
		ID:        credential.ID,
		PublicKey: credential.PublicKey,
		Label:     "Passkey",
	})
	if err := h.credStore.Save(); err != nil {
		log.Printf("webauthn: save credential error: %v", err)
		http.Error(w, `{"error":"failed to save credential"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// LoginBegin starts the WebAuthn login ceremony.
func (h *WebAuthnHandler) LoginBegin(w http.ResponseWriter, r *http.Request) {
	user := h.user()
	if len(user.credentials) == 0 {
		http.Error(w, `{"error":"no credentials registered"}`, http.StatusBadRequest)
		return
	}

	assertion, session, err := h.wa.BeginLogin(user)
	if err != nil {
		log.Printf("webauthn: begin login error: %v", err)
		http.Error(w, `{"error":"login failed"}`, http.StatusInternalServerError)
		return
	}

	h.mu.Lock()
	h.cleanExpiredChallenges()
	h.challenges[session.Challenge] = challengeEntry{
		session:   session,
		createdAt: time.Now(),
	}
	h.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(assertion)
}

// LoginFinish completes the WebAuthn login ceremony and sets a session cookie.
func (h *WebAuthnHandler) LoginFinish(w http.ResponseWriter, r *http.Request) {
	user := h.user()

	h.mu.Lock()
	h.cleanExpiredChallenges()
	var matchedKey string
	var sessionData *webauthn.SessionData
	for k, v := range h.challenges {
		matchedKey = k
		sessionData = v.session
		break
	}
	h.mu.Unlock()

	if sessionData == nil {
		http.Error(w, `{"error":"no pending login"}`, http.StatusBadRequest)
		return
	}

	_, err := h.wa.FinishLogin(user, *sessionData, r)
	if err != nil {
		log.Printf("webauthn: finish login error: %v", err)
		http.Error(w, `{"error":"login verification failed"}`, http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	delete(h.challenges, matchedKey)
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
