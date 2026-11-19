package projects

import (
	"context"
	"errors"
	"fmt"

	"github.com/lsegal/aviary/internal/projects/browser"
	"github.com/lsegal/aviary/internal/projects/core"
)

// errNoBrowserDelivery reports a sign-in action while GitHub is not read
// through Aviary's browser.
var errNoBrowserDelivery = errors.New("projects are not reading GitHub through Aviary's browser: browser delivery is not running")

// browserRun returns the running engine when it reads through the browser.
func (m *Manager) browserRun() (*engineRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running == nil || m.running.signIn == nil {
		return nil, errNoBrowserDelivery
	}
	return m.running, nil
}

// GitHubSession reports browser delivery's GitHub sign-in, the zero session
// when GitHub is read some other way.
func (m *Manager) GitHubSession() core.GitHubSession {
	run, err := m.browserRun()
	if err != nil {
		return core.GitHubSession{}
	}
	return run.signIn.State()
}

// GitHubSignIn opens GitHub's sign-in page in Aviary's browser, visibly, and
// returns at once; reads resume in this run once the sign-in lands, and the
// session state reports progress. Where no window can appear it fails at once
// with what to do instead.
func (m *Manager) GitHubSignIn() (map[string]any, error) {
	run, err := m.browserRun()
	if err != nil {
		return nil, err
	}
	state := run.signIn.State()
	if state.WindowUnavailable != "" {
		_, err := run.signIn.SignIn(context.Background())
		return nil, err
	}
	if state.SigningIn {
		return nil, browser.ErrSigningIn
	}
	go func() {
		if login, err := run.signIn.SignIn(context.Background()); err == nil {
			_, _ = m.Refresh("")
			m.logf("signed in to GitHub as %s", login)
		}
	}()
	return map[string]any{"opened": true, "url": githubLoginURL}, nil
}

// ImportBrowserProfile copies the GitHub sign-in from an Aviary browser profile
// signed in elsewhere into the profile browser delivery reads with, for
// machines where no sign-in window can appear. A running browser is restarted
// so its next read is signed in.
func (m *Manager) ImportBrowserProfile(from string) (map[string]any, error) {
	if from == "" {
		return nil, errors.New("profile is required: the path of an Aviary browser profile signed in to GitHub")
	}
	m.mu.Lock()
	options := BrowserOptionsFrom(m.browser)
	run := m.running
	m.mu.Unlock()
	if run == nil || run.driver == nil {
		if err := ImportBrowserProfile(from, options.Config()); err != nil {
			return nil, err
		}
		return map[string]any{"imported": true}, nil
	}
	// The browser saves its own (signed-out) session as it closes, so the
	// import goes in only once it has stopped.
	if err := run.driver.Suspend(); err != nil {
		return nil, fmt.Errorf("stop Aviary's browser to import the profile: %w", err)
	}
	importErr := ImportBrowserProfile(from, run.settings.Browser.Config())
	if err := run.driver.Resume(); err != nil {
		return nil, fmt.Errorf("restart Aviary's browser after the import: %w", err)
	}
	if importErr != nil {
		return nil, importErr
	}
	_, _ = m.Refresh("")
	return map[string]any{"imported": true}, nil
}
