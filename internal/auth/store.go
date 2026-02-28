// Package auth manages credential storage for Aviary.
package auth

import "fmt"

// Store is the interface for credential storage backends.
type Store interface {
	// Set stores a credential under the given key.
	Set(key, value string) error
	// Get retrieves a credential by key.
	Get(key string) (string, error)
	// Delete removes a credential by key.
	Delete(key string) error
	// List returns all stored credential keys.
	List() ([]string, error)
}

// ErrNotFound is returned when a credential key is not found.
type ErrNotFound struct {
	Key string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("credential %q not found", e.Key)
}

// IsNotFound reports whether err is an ErrNotFound.
func IsNotFound(err error) bool {
	_, ok := err.(*ErrNotFound)
	return ok
}
