package auth

import (
	"fmt"
	"sort"
	"sync"

	"github.com/zalando/go-keyring"
)

const keychainService = "aviary"

// KeychainStore uses the system keychain (macOS/Windows/Linux) as a backend.
type KeychainStore struct {
	mu   sync.RWMutex
	keys []string // tracked in-memory for List(); keychain has no enumerate API
}

// NewKeychainStore returns a new KeychainStore.
func NewKeychainStore() *KeychainStore {
	return &KeychainStore{}
}

// Set stores a credential in the system keychain.
func (s *KeychainStore) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := keyring.Set(keychainService, key, value); err != nil {
		return fmt.Errorf("keychain set %q: %w", key, err)
	}
	// Track key for List().
	for _, k := range s.keys {
		if k == key {
			return nil
		}
	}
	s.keys = append(s.keys, key)
	sort.Strings(s.keys)
	return nil
}

// Get retrieves a credential from the system keychain.
func (s *KeychainStore) Get(key string) (string, error) {
	val, err := keyring.Get(keychainService, key)
	if err != nil {
		return "", &ErrNotFound{Key: key}
	}
	return val, nil
}

// Delete removes a credential from the system keychain.
func (s *KeychainStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := keyring.Delete(keychainService, key); err != nil {
		return fmt.Errorf("keychain delete %q: %w", key, err)
	}
	// Remove from tracked keys.
	for i, k := range s.keys {
		if k == key {
			s.keys = append(s.keys[:i], s.keys[i+1:]...)
			break
		}
	}
	return nil
}

// List returns all tracked credential keys.
func (s *KeychainStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.keys))
	copy(out, s.keys)
	return out, nil
}
