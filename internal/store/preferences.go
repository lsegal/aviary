package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// NavPreferences is the web UI's customized navigation (#1003): per list of
// navigation items (agent pages, System pages, the top row), the user's
// order and the items they hid. Items the app adds later are not listed and
// take their default place.
type NavPreferences struct {
	Order  map[string][]string `json:"order"`
	Hidden map[string][]string `json:"hidden"`
}

// Preferences holds the user's web UI preferences, kept on the server so
// they follow the user across browsers and devices.
type Preferences struct {
	Navigation NavPreferences `json:"navigation"`
}

var preferencesMu sync.Mutex

// PreferencesPath returns the on-disk location of the user's preferences.
func PreferencesPath() string {
	return filepath.Join(DataDir(), "preferences.json")
}

// ReadPreferences loads the user's preferences, returning empty ones if absent.
func ReadPreferences() (*Preferences, error) {
	preferencesMu.Lock()
	defer preferencesMu.Unlock()
	return readPreferences()
}

func readPreferences() (*Preferences, error) {
	prefs := &Preferences{}
	data, err := os.ReadFile(PreferencesPath())
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading preferences: %w", err)
	}
	if err == nil {
		if err := json.Unmarshal(data, prefs); err != nil {
			return nil, fmt.Errorf("parsing preferences: %w", err)
		}
	}
	if prefs.Navigation.Order == nil {
		prefs.Navigation.Order = map[string][]string{}
	}
	if prefs.Navigation.Hidden == nil {
		prefs.Navigation.Hidden = map[string][]string{}
	}
	return prefs, nil
}

// UpdatePreferences applies fn to the stored preferences and saves the result.
func UpdatePreferences(fn func(*Preferences)) (*Preferences, error) {
	preferencesMu.Lock()
	defer preferencesMu.Unlock()
	prefs, err := readPreferences()
	if err != nil {
		return nil, err
	}
	fn(prefs)
	if err := os.MkdirAll(DataDir(), 0o700); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling preferences: %w", err)
	}
	if err := os.WriteFile(PreferencesPath(), data, 0o600); err != nil {
		return nil, fmt.Errorf("writing preferences: %w", err)
	}
	return prefs, nil
}
