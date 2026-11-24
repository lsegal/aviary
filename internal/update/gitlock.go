package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

// updaterLockName is the file in the checkout's git directory that serializes
// updater git work across every Aviary process on that checkout: the server,
// `aviary watch`, and `aviary upgrade`.
const updaterLockName = "aviary-update.lock"

// staleIndexLockAge is how old .git/index.lock must be before the updater
// calls it stale rather than held by a running git command.
const staleIndexLockAge = 10 * time.Minute

// ErrGitBusy reports that another git command kept a lock in the checkout
// for longer than the updater's retries waited.
var ErrGitBusy = errors.New("another git operation is running in the checkout")

// StaleLockError reports a .git/index.lock old enough that no running git
// command is likely to own it. RemoveStaleIndexLock clears it.
type StaleLockError struct {
	Path string
	Age  time.Duration
}

func (e *StaleLockError) Error() string {
	return fmt.Sprintf("a stale git lock (%s, %s old) is blocking updates", e.Path, e.Age.Round(time.Minute))
}

// Error codes for git-mode update failures the dashboard explains itself.
const (
	ErrCodeGitBusy      = "git-busy"
	ErrCodeGitStaleLock = "git-stale-lock"
)

// ErrorCode classifies an update failure as one of the ErrCode* constants,
// or "" for any other failure.
func ErrorCode(err error) string {
	var stale *StaleLockError
	switch {
	case errors.As(err, &stale):
		return ErrCodeGitStaleLock
	case errors.Is(err, ErrGitBusy):
		return ErrCodeGitBusy
	}
	return ""
}

type callerKey struct{}

// WithCaller names who runs the updater git commands made with ctx, so the
// logs show where lock contention comes from.
func WithCaller(ctx context.Context, caller string) context.Context {
	return context.WithValue(ctx, callerKey{}, caller)
}

func callerOf(ctx context.Context) string {
	if c, ok := ctx.Value(callerKey{}).(string); ok && c != "" {
		return c
	}
	return "unknown"
}

// updaterSem serializes updater git work within this process; the file lock
// in withUpdaterLock extends that to other processes.
var updaterSem = make(chan struct{}, 1)

// lockPoll is how often a blocked updater retries the cross-process lock.
var lockPoll = 100 * time.Millisecond

// gitDir returns the checkout's git directory, shared by all its worktrees.
func gitDir(ctx context.Context, dir string) string {
	out, err := runGit(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || out == "" {
		return filepath.Join(dir, ".git")
	}
	return out
}

// withUpdaterLock runs fn while holding the updater lock for dir, waiting
// until ctx is done for another updater, in this or another process, to
// finish its git work.
func withUpdaterLock(ctx context.Context, dir string, fn func() error) error {
	select {
	case updaterSem <- struct{}{}:
	case <-ctx.Done():
		return fmt.Errorf("waiting for another update check: %w", ctx.Err())
	}
	defer func() { <-updaterSem }()

	path := filepath.Join(gitDir(ctx, dir), updaterLockName)
	lock := flock.New(path)
	ok, err := lock.TryLock()
	if err == nil && !ok {
		holder, _ := os.ReadFile(path)
		slog.Info("update: waiting for another process's updater git work", "component", "update",
			"caller", callerOf(ctx), "holder", strings.TrimSpace(string(holder)), "lock", path)
		ok, err = lock.TryLockContext(ctx, lockPoll)
	}
	if err != nil {
		return fmt.Errorf("taking the updater lock %s: %w", path, err)
	}
	if !ok {
		return fmt.Errorf("taking the updater lock %s: %w", path, ctx.Err())
	}
	defer lock.Unlock() //nolint:errcheck
	_ = os.WriteFile(path, fmt.Appendf(nil, "pid %d (%s)\n", os.Getpid(), callerOf(ctx)), 0o600)
	return fn()
}

// lockRetryDelays are the base waits between attempts of a git command that
// failed because another git command held one of the repository's locks.
var lockRetryDelays = []time.Duration{
	250 * time.Millisecond, 500 * time.Millisecond, time.Second,
	2 * time.Second, 4 * time.Second, 8 * time.Second,
}

var lockFileError = regexp.MustCompile(`Unable to create '([^']+\.lock)': File exists`)

// runGitRetrying runs a git command that takes repository locks, retrying
// with jittered backoff while another git command holds one. Once the
// retries run out it reports ErrGitBusy, or a StaleLockError when the lock
// is an index.lock too old to belong to a running command.
func runGitRetrying(ctx context.Context, dir string, args ...string) (string, error) {
	for attempt := 0; ; attempt++ {
		out, err := runGit(ctx, dir, args...)
		m := lockFileError.FindStringSubmatch(errString(err))
		if m == nil {
			return out, err
		}
		if attempt == len(lockRetryDelays) {
			slog.Warn("update: git lock still held after retries", "component", "update",
				"caller", callerOf(ctx), "args", strings.Join(args, " "), "lock", m[1], "error", err)
			if info, statErr := os.Stat(m[1]); statErr == nil && filepath.Base(m[1]) == "index.lock" {
				if age := time.Since(info.ModTime()); age >= staleIndexLockAge {
					return "", &StaleLockError{Path: m[1], Age: age}
				}
			}
			return "", fmt.Errorf("%w (%s)", ErrGitBusy, m[1])
		}
		base := lockRetryDelays[attempt]
		wait := base/2 + rand.N(base)
		slog.Info("update: git lock held, retrying", "component", "update",
			"caller", callerOf(ctx), "args", strings.Join(args, " "), "lock", m[1], "attempt", attempt+1, "wait", wait)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return "", fmt.Errorf("%w (%s)", ErrGitBusy, m[1])
		}
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// RemoveStaleIndexLock removes the checkout's .git/index.lock when it is at
// least staleIndexLockAge old, holding the updater lock so no updater git
// command races the removal. A younger lock may belong to a running git
// command, so it is left alone and reported as ErrGitBusy.
func RemoveStaleIndexLock(ctx context.Context, dir string) error {
	return withUpdaterLock(ctx, dir, func() error {
		path := filepath.Join(gitDir(ctx, dir), "index.lock")
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if age := time.Since(info.ModTime()); age < staleIndexLockAge {
			return fmt.Errorf("%w (%s is only %s old)", ErrGitBusy, path, age.Round(time.Second))
		}
		slog.Warn("update: removing stale git index lock", "component", "update", "caller", callerOf(ctx), "lock", path, "age", time.Since(info.ModTime()))
		return os.Remove(path)
	})
}
