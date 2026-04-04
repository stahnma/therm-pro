package auth

import (
	"path/filepath"
	"testing"
)

func TestCredentialStorage_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passkeys.json")

	store := NewCredentialStore(path)
	cred := StoredCredential{
		ID:        []byte("test-credential-id"),
		PublicKey: []byte("test-public-key"),
		Label:     "My 1Password Key",
	}
	store.Add(cred)
	if err := store.Save(); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	store2 := NewCredentialStore(path)
	if err := store2.Load(); err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(store2.Credentials()) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(store2.Credentials()))
	}
}

func TestCredentialStorage_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passkeys.json")

	store := NewCredentialStore(path)
	if err := store.Load(); err != nil {
		t.Fatalf("load of nonexistent file should not error: %v", err)
	}
	if len(store.Credentials()) != 0 {
		t.Fatalf("expected 0 credentials, got %d", len(store.Credentials()))
	}
}
