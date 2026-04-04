package auth

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

// StoredCredential represents a persisted WebAuthn credential.
type StoredCredential struct {
	ID             []byte    `json:"id"`
	PublicKey      []byte    `json:"public_key"`
	BackupEligible bool      `json:"backup_eligible"`
	Label          string    `json:"label"`
	CreatedAt      time.Time `json:"created_at"`
}

// CredentialStore persists WebAuthn credentials to a JSON file.
type CredentialStore struct {
	mu    sync.RWMutex
	path  string
	creds []StoredCredential
}

// NewCredentialStore creates a new CredentialStore backed by the given file path.
func NewCredentialStore(path string) *CredentialStore {
	return &CredentialStore{path: path}
}

// Load reads credentials from disk. If the file does not exist, this is a no-op.
func (s *CredentialStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.creds = nil
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &s.creds)
}

// Save writes the current credentials to disk.
func (s *CredentialStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s.creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0600)
}

// Add appends a credential to the store.
func (s *CredentialStore) Add(cred StoredCredential) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cred.CreatedAt.IsZero() {
		cred.CreatedAt = time.Now()
	}
	s.creds = append(s.creds, cred)
}

// Credentials returns a copy of all stored credentials.
func (s *CredentialStore) Credentials() []StoredCredential {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]StoredCredential(nil), s.creds...)
}
