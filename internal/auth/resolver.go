package auth

import (
	"fmt"
	"strings"
)

// Resolve resolves an auth reference string of the form "auth:<provider>:<name>"
// using the provided Store. If ref does not start with "auth:", it is returned as-is
// (treated as a literal value).
func Resolve(store Store, ref string) (string, error) {
	if !strings.HasPrefix(ref, "auth:") {
		return ref, nil
	}

	// Strip "auth:" prefix.
	key := strings.TrimPrefix(ref, "auth:")
	if key == "" {
		return "", fmt.Errorf("invalid auth reference: %q", ref)
	}

	val, err := store.Get(key)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", ref, err)
	}
	return val, nil
}

// ParseRef parses "auth:<provider>:<name>" into its components.
// Returns provider="", name="" if ref is not an auth reference.
func ParseRef(ref string) (provider, name string, ok bool) {
	if !strings.HasPrefix(ref, "auth:") {
		return "", "", false
	}
	rest := strings.TrimPrefix(ref, "auth:")
	parts := strings.SplitN(rest, ":", 2)
	if len(parts) != 2 {
		return parts[0], "", true
	}
	return parts[0], parts[1], true
}
