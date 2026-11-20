package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	aviarybrowser "github.com/lsegal/aviary/internal/browser"
)

// windowClosedPolls is how many reads in a row must find no page open before
// the window counts as closed, so a read that lands between one tab closing and
// the next opening does not shut the browser down under the user.
const windowClosedPolls = 3

// These are variables so tests need not spend real seconds on them or stand up
// a DevTools WebSocket: windowPollInterval is how often the open pages are read,
// windowReadTimeout bounds one read, windowStopTimeout bounds a clean shutdown
// before the process is killed, windowCloseFailedGrace is the shorter wait when
// the shutdown request itself failed, and closeBrowser asks the browser to shut
// down.
var (
	windowPollInterval     = 500 * time.Millisecond
	windowReadTimeout      = 5 * time.Second
	windowStopTimeout      = 10 * time.Second
	windowCloseFailedGrace = 2 * time.Second
	closeBrowser           = aviarybrowser.CloseOverDevTools
)

// OpenWindow launches a visible browser on the profile itself and blocks until
// the user closes its window or ctx is canceled. Nothing drives the browser, so
// it behaves as if the user had started it by hand on that profile.
//
// Unlike launch, it never falls back to a private directory when another Aviary
// instance holds the profile: the point is to change the profile, and changes
// made in a directory removed on exit would be silently lost.
func OpenWindow(ctx context.Context, config Config) error {
	binary, err := aviarybrowser.FindBinary(config.Binary)
	if err != nil {
		return err
	}
	profile, err := ProfileDir(config.Profile)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(profile, 0o700); err != nil {
		return fmt.Errorf("create browser profile directory %s: %w", profile, err)
	}
	lock, err := aviarybrowser.LockProfile(profile)
	if err != nil {
		if errors.Is(err, aviarybrowser.ErrProfileLocked) {
			return fmt.Errorf("%w (%s): stop that run first, or set the browser profile_directory setting to open a different directory", err, profile)
		}
		return err
	}
	defer lock.Release()
	port, err := aviarybrowser.FreePort()
	if err != nil {
		return err
	}
	// Not exec.CommandContext: it kills the browser on cancellation, which loses
	// the configuration changes the browser has not written to disk yet. The
	// supervisor still puts it under the orphan guard, which only acts when
	// Aviary exits without closing the window first.
	cmd := exec.Command(binary, aviarybrowser.LaunchArgs(profile, port, false)...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	started, err := aviarybrowser.StartOwned(config.supervisor(), shutdownPolicy(), cmd)
	if err != nil {
		return fmt.Errorf("start browser (%s): %w", binary, err)
	}
	window := &openWindow{OwnedChild: started, baseURL: aviarybrowser.DebugURL(port)}
	return window.run(ctx, profile)
}

// openWindow is a running browser the user is working in.
type openWindow struct {
	*aviarybrowser.OwnedChild
	baseURL string
	wsURL   string
}

// run waits for the browser to come up and then for the user to finish with it.
func (w *openWindow) run(ctx context.Context, profile string) error {
	readyCtx, cancelReady := context.WithCancel(ctx)
	defer cancelReady()
	go func() {
		select {
		case <-w.Exited():
			cancelReady()
		case <-readyCtx.Done():
		}
	}()
	wsURL, err := aviarybrowser.WaitForReady(readyCtx, w.baseURL, aviarybrowser.OwnedReadyTimeout, aviarybrowser.OwnedReadyInterval)
	if err != nil {
		select {
		case <-w.Exited():
			if ctx.Err() != nil {
				return nil
			}
			// Chrome hands off to a browser already running on the directory and
			// exits without ever serving its DevTools endpoint.
			return fmt.Errorf("the browser exited before it was ready; another browser may already be running on %s", profile)
		default:
		}
		w.stop()
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	w.wsURL = wsURL
	return w.watch(ctx)
}

// watch blocks until the browser has exited. Closing the last window ends the
// process on Linux and Windows, but macOS keeps the application running with no
// windows, so the page list is watched as well: having had a page and then
// having none means the user is done.
func (w *openWindow) watch(ctx context.Context) error {
	ticker := time.NewTicker(windowPollInterval)
	defer ticker.Stop()
	hadPage, empty := false, 0
	for {
		select {
		case <-w.Exited():
			if ctx.Err() == nil && w.WaitErr() != nil {
				return fmt.Errorf("browser exited: %w", w.WaitErr())
			}
			return nil
		case <-ctx.Done():
			w.stop()
			return nil
		case <-ticker.C:
		}
		readCtx, cancelRead := context.WithTimeout(ctx, windowReadTimeout)
		pages, err := aviarybrowser.PageTargets(readCtx, w.baseURL)
		cancelRead()
		if err != nil {
			continue
		}
		if len(pages) > 0 {
			hadPage, empty = true, 0
			continue
		}
		if empty++; hadPage && empty >= windowClosedPolls {
			w.stop()
			return nil
		}
	}
}

// stop shuts the window's browser down cleanly, so the preferences it is still
// holding in memory are written out, and kills it only if it will not go.
func (w *openWindow) stop() { w.OwnedChild.Stop(w.wsURL) }
