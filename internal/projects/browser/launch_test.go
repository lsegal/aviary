package browser

import (
	"testing"

	aviarybrowser "github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/config"
)

func TestProfileDirUsesOverride(t *testing.T) {
	dir, err := ProfileDir("/tmp/custom-profile")
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	if dir != "/tmp/custom-profile" {
		t.Fatalf("profile %q, want the override", dir)
	}
}

// TestProfileDirDefaultsToAviarysBrowserProfile checks Code Loop reads GitHub
// with the same profile Aviary's own browser signs in with.
func TestProfileDirDefaultsToAviarysBrowserProfile(t *testing.T) {
	dir, err := ProfileDir("")
	if err != nil {
		t.Fatalf("ProfileDir: %v", err)
	}
	if want := aviarybrowser.DefaultProfileDir(); dir != want {
		t.Fatalf("profile %q, want Aviary's browser profile %q", dir, want)
	}
}

func TestConfigFromBrowserSettings(t *testing.T) {
	got := ConfigFrom(config.BrowserConfig{Binary: "/opt/chrome", ProfileDir: "/data/profile", Headless: true})
	if got.Binary != "/opt/chrome" || got.Profile != "/data/profile" || got.Headed {
		t.Fatalf("ConfigFrom = %+v", got)
	}
}
