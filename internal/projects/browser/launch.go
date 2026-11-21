package browser

import (
	"context"
	"time"

	aviarybrowser "github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/config"
)

// Config collects the overrides that decide which browser Code Loop drives
// and where it keeps that browser's state. Both are empty by default, which
// means "discover a Chromium-based browser" and "use Aviary's browser
// profile"; ConfigFrom fills them in from Aviary's browser settings.
type Config struct {
	// Binary is an explicit browser executable (the browser `binary` setting).
	Binary string
	// Profile is an explicit profile directory (the browser
	// `profile_directory` setting).
	Profile string
	// Headed launches a visible browser window instead of a headless one. It is
	// how `Aviary auth` puts a login form in front of the user, on the same
	// profile the headless watch loop reads GitHub with.
	Headed bool
	// Supervisor starts, waits on, and kills the browser process. A nil
	// supervisor hands it to Aviary's own child-process tracking, so it cannot
	// outlive the run that started it.
	Supervisor Supervisor
}

const (
	// targetTimeout bounds the wait for the window a headed browser
	// opens by itself to show up in the target list. The browser is already
	// answering the DevTools endpoint by the time it is asked, so the window is
	// normally there on the first read; past this it is not coming.
	targetTimeout = 2 * time.Second
	// commandTimeout bounds a single command sent to a tab: a
	// navigation, a reload, a script evaluation, a screenshot. GitHub pages
	// are large and a run paces its loads, so this is deliberately generous;
	// what it exists to catch is a page or a renderer that has stopped
	// answering altogether, which would otherwise park the poll loop for the
	// rest of the run (issue #472).
	commandTimeout = 2 * time.Minute
)

// tabOpenTimeout bounds opening a tab and enabling the events it is read
// through. It is a variable only so tests need not spend it.
var tabOpenTimeout = 30 * time.Second

// ProfileDir reports the profile directory the browser is launched against:
// the override when one was given, otherwise Aviary's own browser profile, so
// a GitHub sign-in made in Aviary's browser is the one Code Loop reads with.
func ProfileDir(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	return aviarybrowser.DefaultProfileDir(), nil
}

// ConfigFrom builds the launch config from Aviary's browser settings.
func ConfigFrom(settings config.BrowserConfig) Config {
	return Config{Binary: settings.Binary, Profile: settings.ProfileDir}
}

// process is a running browser and the DevTools endpoint it answers on.
type process = aviarybrowser.OwnedBrowser

// launch starts a browser against Aviary's own profile and waits for its
// DevTools endpoint to answer. The browser is started through the config's
// supervisor, so Aviary's orphan guards take it down on every exit path,
// including one that runs no cleanup of its own. When another Aviary instance,
// or Aviary's own browser, already runs on the profile, the browser runs in a
// private directory instead (issue #663).
//
// ctx bounds the launch only. Canceling it later does not kill the browser:
// the run that owns it closes it, cleanly, on its way out, and a kill would
// skip the shutdown that writes the profile to disk (issue #773).
func launch(ctx context.Context, config Config) (*process, error) {
	profile, err := ProfileDir(config.Profile)
	if err != nil {
		return nil, err
	}
	return aviarybrowser.LaunchOwned(ctx, aviarybrowser.OwnedOptions{
		Binary:     config.Binary,
		Profile:    profile,
		Headed:     config.Headed,
		Supervisor: config.supervisor(),
		Shutdown:   shutdownPolicy(),
	})
}
