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

// DisplayPreferences is the web UI's look (#1093): its color theme and text
// size. An empty field means the default, so options added later need no
// migration.
type DisplayPreferences struct {
	// Theme is "light", "dark" or "system" (following the OS).
	Theme string `json:"theme,omitempty"`
	// TextSize is "small", "default", "large" or "xlarge".
	TextSize string `json:"textSize,omitempty"`
}

// NotificationPreferences controls the notification system (#1215): the
// master switch, each delivery method, sound, quiet hours, and each event
// type. Nil in Preferences means DefaultNotificationPreferences.
type NotificationPreferences struct {
	// Enabled is the master switch: off, nothing is delivered anywhere.
	Enabled bool `json:"enabled"`
	// System shows browser or installed-app notifications, once the browser
	// grants permission; InApp shows banners and toasts in the web UI.
	System bool `json:"system"`
	InApp  bool `json:"inApp"`
	// Server has a macOS server show a Notification Center notification
	// itself when no browser is connected.
	Server     bool                         `json:"server"`
	Sound      NotificationSound            `json:"sound"`
	QuietHours NotificationQuietHours       `json:"quietHours"`
	Events     map[string]NotificationEvent `json:"events"`
}

// NotificationSound is how notifications sound. Sound is off by default.
type NotificationSound struct {
	Enabled bool `json:"enabled"`
	// Type is the default sound, one of the web UI's bundled sounds.
	Type string `json:"type"`
	// Volume is 0 to 100.
	Volume int `json:"volume"`
}

// NotificationQuietHours holds back live delivery between Start and End,
// "HH:MM" in the server's local time; notifications still reach the bell.
type NotificationQuietHours struct {
	Enabled bool   `json:"enabled"`
	Start   string `json:"start"`
	End     string `json:"end"`
}

// NotificationEvent configures one event type.
type NotificationEvent struct {
	Enabled bool `json:"enabled"`
	// Delivery is "big" (a banner across the top), "small" (a corner toast)
	// or "system" (a system notification, or a toast without permission).
	Delivery string `json:"delivery"`
	// Sound is the event's own sound, "" for the default one, or "none".
	Sound string `json:"sound,omitempty"`
}

// Preferences holds the user's web UI preferences, kept on the server so
// they follow the user across browsers and devices.
type Preferences struct {
	Navigation    NavPreferences           `json:"navigation"`
	Display       DisplayPreferences       `json:"display"`
	Notifications *NotificationPreferences `json:"notifications,omitempty"`
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
