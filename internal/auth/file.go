package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// FileStore is a JSON-file-backed auth store.
type FileStore struct {
	path string
	mu   sync.RWMutex
	data map[string]string
}

// NewFileStore creates a FileStore backed by path.
// The file is created if it does not exist.
func NewFileStore(path string) (*FileStore, error) {
	s := &FileStore{path: path, data: make(map[string]string)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading auth file %s: %w", s.path, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, &s.data)
}

func (s *FileStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// Set stores a credential.
func (s *FileStore) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return s.save()
}

// Get retrieves a credential.
func (s *FileStore) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[key]
	if !ok {
		return "", &ErrNotFound{Key: key}
	}
	return v, nil
}

// Delete removes a credential.
func (s *FileStore) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[key]; !ok {
		return &ErrNotFound{Key: key}
	}
	delete(s.data, key)
	return s.save()
}

// List returns all stored keys sorted.
func (s *FileStore) List() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}
