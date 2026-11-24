package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/config"
)

// profileLockName is the file, inside a browser profile, that a Aviary
// instance holds locked for as long as its browser runs on that profile. The
// lock is the operating system's own, so it is released when the process
// exits however it exits, and a crashed run never leaves the profile claimed.
const profileLockName = "aviary-browser.lock"

// profileOwnerName is the file, beside the lock, that records which process
// holds it, so a refused claim can name the holder. It is a separate file
// because Windows refuses reads of a locked byte range to other processes.
const profileOwnerName = "aviary-browser-owner.json"

// ErrProfileLocked reports a profile another running Aviary instance already
// has a browser on.
var ErrProfileLocked = errors.New("browser profile is in use by another aviary instance")

// processStarted is when this process started, recorded in the lock owner so
// a refused claim can tell the user which run holds the profile.
var processStarted = time.Now()

// ProfileOwner describes the process holding a profile lock.
type ProfileOwner struct {
	PID       int       `json:"pid"`
	Hostname  string    `json:"hostname,omitempty"`
	Started   time.Time `json:"started"`
	ConfigDir string    `json:"config_dir,omitempty"`
	// DevTools is the DevTools endpoint of the browser the holder runs on the
	// profile, once it answers.
	DevTools string `json:"devtools,omitempty"`
}

// String names the owner for an error message or a status line.
func (o ProfileOwner) String() string {
	parts := []string{fmt.Sprintf("pid %d", o.PID)}
	if o.Hostname != "" {
		parts = append(parts, "host "+o.Hostname)
	}
	if !o.Started.IsZero() {
		parts = append(parts, "started "+o.Started.Local().Format(time.RFC3339))
	}
	if o.ConfigDir != "" {
		parts = append(parts, "config dir "+o.ConfigDir)
	}
	if o.DevTools != "" {
		parts = append(parts, "DevTools "+o.DevTools)
	}
	return strings.Join(parts, ", ")
}

// alive reports whether the owner's process is still running. An owner on
// another host cannot be checked and counts as alive.
func (o ProfileOwner) alive() bool {
	if host, err := os.Hostname(); err != nil || o.Hostname == "" || o.Hostname != host {
		return true
	}
	return processAlive(o.PID)
}

// ProfileLockedError is a claim refused because the profile is already held.
// It matches ErrProfileLocked.
type ProfileLockedError struct {
	// Owner is the holder, nil when it left no record.
	Owner *ProfileOwner
	// Self reports that this process holds the profile itself, for example
	// for a Code Loop or Projects sign-in browser.
	Self bool
}

func (e *ProfileLockedError) Error() string {
	if e.Self {
		return "browser profile is in use by another browser of this aviary instance"
	}
	if e.Owner == nil {
		return ErrProfileLocked.Error()
	}
	if !e.Owner.alive() {
		return fmt.Sprintf("%v [%v, no longer running; a process it started may still hold the lock]", ErrProfileLocked, e.Owner)
	}
	return fmt.Sprintf("%v [%v]", ErrProfileLocked, e.Owner)
}

// Is makes the error match ErrProfileLocked.
func (e *ProfileLockedError) Is(target error) bool { return target == ErrProfileLocked }

// heldProfiles are the profiles this process holds, by profileKey. The
// operating system's lock belongs to an open file, not a process, so without
// this a second claim from this same process would be refused as if another
// instance held the profile (issue #1171).
var (
	heldProfilesMu sync.Mutex
	heldProfiles   = map[string]*ProfileLock{}
)

// profileKey is the form a profile path is tracked under, so two spellings of
// one directory are one profile.
func profileKey(profile string) string {
	if abs, err := filepath.Abs(profile); err == nil {
		profile = abs
	}
	if resolved, err := filepath.EvalSymlinks(profile); err == nil {
		profile = resolved
	}
	return filepath.Clean(profile)
}

// ProfileLock is a held claim on a browser profile.
type ProfileLock struct {
	file    *os.File
	profile string
	key     string
	owner   ProfileOwner
}

// LockProfile claims a profile for this process's browser, failing with a
// *ProfileLockedError, which matches ErrProfileLocked, when this or another
// instance already holds it.
func LockProfile(profile string) (*ProfileLock, error) {
	key := profileKey(profile)
	heldProfilesMu.Lock()
	defer heldProfilesMu.Unlock()
	if held := heldProfiles[key]; held != nil {
		owner := held.owner
		return nil, &ProfileLockedError{Owner: &owner, Self: true}
	}
	file, err := os.OpenFile(filepath.Join(profile, profileLockName), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open browser profile lock: %w", err)
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		if errors.Is(err, ErrProfileLocked) {
			return nil, &ProfileLockedError{Owner: readProfileOwner(profile)}
		}
		return nil, err
	}
	if previous := readProfileOwner(profile); previous != nil && previous.PID != os.Getpid() {
		slog.Info("browser: reclaimed profile lock left by an exited run", "profile", profile, "owner", previous.String())
	}
	host, _ := os.Hostname()
	lock := &ProfileLock{file: file, profile: profile, key: key, owner: ProfileOwner{
		PID:       os.Getpid(),
		Hostname:  host,
		Started:   processStarted,
		ConfigDir: filepath.Dir(config.DefaultPath()),
	}}
	lock.writeOwner()
	heldProfiles[key] = lock
	return lock, nil
}

// SetDevTools records the DevTools endpoint of the browser running on the
// held profile, so another claim from this process can reuse that browser
// instead of failing, and one from another instance can name it.
func (l *ProfileLock) SetDevTools(baseURL string) {
	if l == nil {
		return
	}
	heldProfilesMu.Lock()
	defer heldProfilesMu.Unlock()
	if l.file == nil {
		return
	}
	l.owner.DevTools = baseURL
	l.writeOwner()
}

// writeOwner records the holder beside the lock. A failure only costs the
// detail in a refused claim's message, so it is logged, not returned.
func (l *ProfileLock) writeOwner() {
	data, err := json.Marshal(l.owner)
	if err == nil {
		err = os.WriteFile(filepath.Join(l.profile, profileOwnerName), data, 0o600)
	}
	if err != nil {
		slog.Debug("browser: recording profile lock owner failed", "profile", l.profile, "err", err)
	}
}

// Release gives the profile up. Closing the file drops the lock with it.
func (l *ProfileLock) Release() {
	if l == nil {
		return
	}
	heldProfilesMu.Lock()
	defer heldProfilesMu.Unlock()
	if l.file == nil {
		return
	}
	if heldProfiles[l.key] == l {
		delete(heldProfiles, l.key)
	}
	_ = os.Remove(filepath.Join(l.profile, profileOwnerName))
	_ = l.file.Close()
	l.file = nil
}

// readProfileOwner reads the holder recorded in profile, nil when there is
// none or it cannot be read.
func readProfileOwner(profile string) *ProfileOwner {
	data, err := os.ReadFile(filepath.Join(profile, profileOwnerName))
	if err != nil {
		return nil
	}
	var owner ProfileOwner
	if json.Unmarshal(data, &owner) != nil || owner.PID <= 0 {
		return nil
	}
	return &owner
}

// sameProcessBrowser returns the DevTools endpoint of a browser this process
// already runs on profile, or "" when it runs none there.
func sameProcessBrowser(profile string) string {
	heldProfilesMu.Lock()
	defer heldProfilesMu.Unlock()
	if held := heldProfiles[profileKey(profile)]; held != nil {
		return held.owner.DevTools
	}
	return ""
}

// ProfileStatus is the state of a browser profile's lock, for diagnostics.
type ProfileStatus struct {
	// Held reports that a running Aviary instance holds the profile.
	Held bool
	// Owner is the holder when Held, or the exited run whose record was
	// cleared when Reclaimed.
	Owner *ProfileOwner
	// Reclaimed reports that a record left by an exited run was cleared.
	Reclaimed bool
	// ChromeRunning reports that a browser, such as Aviary's detached one,
	// runs on the profile without holding the lock.
	ChromeRunning bool
}

// InspectProfile reports who holds profile. A record left by a run that has
// exited is cleared along the way, since the lock itself is already free.
func InspectProfile(profile string) (ProfileStatus, error) {
	if _, err := os.Stat(profile); err != nil {
		if os.IsNotExist(err) {
			return ProfileStatus{}, nil
		}
		return ProfileStatus{}, err
	}
	previous := readProfileOwner(profile)
	lock, err := LockProfile(profile)
	var locked *ProfileLockedError
	if errors.As(err, &locked) {
		return ProfileStatus{Held: true, Owner: locked.Owner}, nil
	}
	if err != nil {
		return ProfileStatus{}, err
	}
	lock.Release()
	status := ProfileStatus{ChromeRunning: chromeHoldsProfile(profile)}
	if previous != nil && previous.PID != os.Getpid() {
		status.Owner, status.Reclaimed = previous, true
	}
	return status, nil
}

// ClaimProfile decides which user-data directory a launch runs the browser
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
// is already running on the profile: Code Loop shares that profile by default,
// so it runs beside that browser in a private directory instead.
func ClaimProfile(profile string) (*ProfileLock, string, error) {
	lock, err := LockProfile(profile)
	if err == nil {
		if !chromeHoldsProfile(profile) {
			return lock, profile, nil
		}
		lock.Release()
	} else if !errors.Is(err, ErrProfileLocked) {
		return nil, "", err
	}
	dataDir, err := os.MkdirTemp("", "aviary-browser-")
	if err != nil {
		return nil, "", fmt.Errorf("create private browser profile directory: %w", err)
	}
	return nil, dataDir, nil
}
