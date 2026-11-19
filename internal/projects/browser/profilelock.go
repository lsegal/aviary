package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// profileLockName is the file, inside a browser profile, that a Aviary
// instance holds locked for as long as its browser runs on that profile. The
// lock is the operating system's own, so it is released when the process
// exits however it exits, and a crashed run never leaves the profile claimed.
const profileLockName = "aviary-browser.lock"

// errProfileLocked reports a profile another running Aviary instance already
// has a browser on.
var errProfileLocked = errors.New("browser profile is in use by another aviary instance")

// profileLock is a held claim on a browser profile.
type profileLock struct {
	file *os.File
}

// lockProfile claims a profile for this process's browser, failing with
// errProfileLocked when another instance already holds it.
func lockProfile(profile string) (*profileLock, error) {
	file, err := os.OpenFile(filepath.Join(profile, profileLockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open browser profile lock: %w", err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &profileLock{file: file}, nil
}

// release gives the profile up. Closing the file drops the lock with it.
func (l *profileLock) release() {
	if l == nil || l.file == nil {
		return
	}
	_ = l.file.Close()
	l.file = nil
}

// claimProfile decides which user-data directory a launch runs the browser
// against. Chrome allows one process per --user-data-dir, and a second one
// launched on a directory already in use hands itself to the first and exits,
// so a second Aviary instance on the same profile never saw its own DevTools
// endpoint come up and failed with "context deadline exceeded" (issue #663).
// The first instance takes the profile itself; any other runs its browser in a
// private directory of its own, and still starts signed in because the saved
// sign-in is read from and written to the shared profile, not the directory
// the browser happens to run in.
//
// The same applies when Aviary's own browser (the one its browser tools drive)
// is already running on the profile: Projects shares that profile by default,
// so it runs beside that browser in a private directory instead.
func claimProfile(profile string) (*profileLock, string, error) {
	lock, err := lockProfile(profile)
	if err == nil {
		if !browserHoldsProfile(profile) {
			return lock, profile, nil
		}
		lock.release()
	} else if !errors.Is(err, errProfileLocked) {
		return nil, "", err
	}
	dataDir, err := os.MkdirTemp("", "aviary-browser-")
	if err != nil {
		return nil, "", fmt.Errorf("create private browser profile directory: %w", err)
	}
	return nil, dataDir, nil
}
