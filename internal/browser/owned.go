package browser

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/chromedp/cdproto"
	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/chromedp"
)

// Supervisor starts, waits on, and kills an owned browser subprocess, so the
// caller decides how a browser is tied to its own lifetime. Chrome allows one
// process per --user-data-dir, and a browser left running holds the profile
// against the next launch.
type Supervisor interface {
	// Start starts cmd as an owned subprocess.
	Start(cmd *exec.Cmd) error
	// Wait waits for a started subprocess to exit and stops owning it.
	Wait(cmd *exec.Cmd) error
	// Kill ends a started subprocess and everything it spawned at once. It
	// only signals, so it is safe while another goroutine is in Wait.
	Kill(cmd *exec.Cmd) error
}

// execSupervisor runs the browser as a plain child process, for callers that
// hand in no supervisor of their own.
type execSupervisor struct{}

func (execSupervisor) Start(cmd *exec.Cmd) error { return cmd.Start() }
func (execSupervisor) Wait(cmd *exec.Cmd) error  { return cmd.Wait() }
func (execSupervisor) Kill(cmd *exec.Cmd) error  { return cmd.Process.Kill() }

// ShutdownPolicy is how an owned browser is stopped: asked over DevTools with
// Close, then killed if it has not exited within Timeout, or within the shorter
// FailedGrace when the request itself failed. Zero values use the defaults.
type ShutdownPolicy struct {
	Timeout     time.Duration
	FailedGrace time.Duration
	Close       func(ctx context.Context, wsURL string) error
}

func (p ShutdownPolicy) withDefaults() ShutdownPolicy {
	if p.Timeout <= 0 {
		p.Timeout = 10 * time.Second
	}
	if p.FailedGrace <= 0 {
		p.FailedGrace = 2 * time.Second
	}
	if p.Close == nil {
		p.Close = CloseOverDevTools
	}
	return p
}

// OwnedChild is a browser process Aviary started and is waiting on.
type OwnedChild struct {
	cmd        *exec.Cmd
	supervisor Supervisor
	shutdown   ShutdownPolicy
	// exited is closed once the process is gone, and waitErr is valid after it.
	exited  chan struct{}
	waitErr error
}

// StartOwned starts cmd through supervisor and waits on it in the background,
// so a shutdown can wait for the exit with a deadline. A nil supervisor runs
// cmd as a plain child process.
func StartOwned(supervisor Supervisor, shutdown ShutdownPolicy, cmd *exec.Cmd) (*OwnedChild, error) {
	if supervisor == nil {
		supervisor = execSupervisor{}
	}
	if err := supervisor.Start(cmd); err != nil {
		return nil, err
	}
	c := &OwnedChild{cmd: cmd, supervisor: supervisor, shutdown: shutdown.withDefaults(), exited: make(chan struct{})}
	go func() {
		c.waitErr = supervisor.Wait(cmd)
		close(c.exited)
	}()
	return c, nil
}

// Exited is closed once the process has exited.
func (c *OwnedChild) Exited() <-chan struct{} { return c.exited }

// WaitErr is how the process exited. It is valid once Exited is closed.
func (c *OwnedChild) WaitErr() error { return c.waitErr }

// Stop asks the browser to shut down over its DevTools WebSocket and waits for
// it to do so, because only a clean shutdown writes out the profile state it is
// still holding in memory. The process is killed only if it is still there when
// the wait runs out, or at once when there is no endpoint to ask because the
// browser never came up. A request that fails gets a shorter wait: the browser
// may already be on its way out, but nothing is coming to shut it down.
func (c *OwnedChild) Stop(wsURL string) {
	if c == nil {
		return
	}
	select {
	case <-c.exited:
		return
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), c.shutdown.Timeout)
	defer cancel()
	wait := ctx.Done()
	if wsURL == "" {
		cancel()
	} else if err := c.shutdown.Close(ctx, wsURL); err != nil {
		failed, cancelFailed := context.WithTimeout(ctx, c.shutdown.FailedGrace)
		defer cancelFailed()
		wait = failed.Done()
	}
	select {
	case <-c.exited:
	case <-wait:
		_ = c.supervisor.Kill(c.cmd)
		<-c.exited
	}
}

// CloseOverDevTools sends the browser its own clean-shutdown command. It
// writes the message to the browser's WebSocket by hand because a window with
// no pages left has nothing for chromedp to attach to, creating a tab to send
// it from would flash a new window, and chromedp's Browser refuses to send this
// command itself.
func CloseOverDevTools(ctx context.Context, wsURL string) error {
	connection, err := chromedp.DialContext(ctx, wsURL)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	return connection.Write(ctx, &cdproto.Message{ID: 1, Method: cdproto.MethodType(cdpbrowser.CommandClose)})
}

// OwnedOptions configures an owned browser launch.
type OwnedOptions struct {
	// Binary is an explicit browser executable; empty discovers one.
	Binary string
	// Profile is the profile directory the browser is launched for.
	Profile string
	// Headed launches a visible window instead of a headless browser.
	Headed bool
	// Supervisor ties the process to its owner; nil runs a plain child.
	Supervisor Supervisor
	// Shutdown is how the browser is stopped.
	Shutdown ShutdownPolicy
}

// OwnedBrowser is a browser Aviary launched for itself, beside the detached
// one Manager shares: it answers on a random free port, runs as a supervised
// child that dies with its owner, and is shut down cleanly over DevTools.
type OwnedBrowser struct {
	Child *OwnedChild
	Port  int
	WSURL string
	// Profile is the profile the browser was launched for, so the saved
	// sign-in is kept in one place whichever directory the browser runs in.
	Profile string
	// DataDir is the --user-data-dir the browser actually runs in: Profile
	// itself when this instance holds it, or a private directory of its own
	// when another Aviary instance or Aviary's own browser already does.
	DataDir string
	// lock is this instance's claim on Profile, nil when it runs in a private
	// directory instead.
	lock *ProfileLock
}

// LaunchOwned starts an owned browser and waits for its DevTools endpoint to
// answer. ctx bounds the launch only: canceling it later does not kill the
// browser, because the owner closes it cleanly on its way out and a kill would
// skip the shutdown that writes the profile to disk.
func LaunchOwned(ctx context.Context, options OwnedOptions) (*OwnedBrowser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	binary, err := FindBinary(options.Binary)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(options.Profile, 0o700); err != nil {
		return nil, fmt.Errorf("create browser profile directory %s: %w", options.Profile, err)
	}
	lock, dataDir, err := ClaimProfile(options.Profile)
	if err != nil {
		return nil, err
	}
	owned := &OwnedBrowser{Profile: options.Profile, DataDir: dataDir, lock: lock}
	port, err := FreePort()
	if err != nil {
		owned.Release()
		return nil, err
	}
	cmd := exec.Command(binary, LaunchArgs(dataDir, port, !options.Headed)...)
	// The browser writes a steady stream of diagnostics to standard error that
	// would otherwise be painted over Aviary's terminal UI.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	child, err := StartOwned(options.Supervisor, options.Shutdown, cmd)
	if err != nil {
		owned.Release()
		return nil, fmt.Errorf("start browser (%s): %w", binary, err)
	}
	owned.Child, owned.Port = child, port
	wsURL, err := WaitForReady(ctx, DebugURL(port), OwnedReadyTimeout, OwnedReadyInterval)
	if err != nil {
		owned.Stop()
		return nil, err
	}
	owned.WSURL = wsURL
	return owned, nil
}

// BaseURL is the browser's DevTools endpoint.
func (b *OwnedBrowser) BaseURL() string { return DebugURL(b.Port) }

// Stop shuts the browser down, cleanly when it will go, and then gives up
// whatever it was run in.
func (b *OwnedBrowser) Stop() {
	b.Child.Stop(b.WSURL)
	b.Release()
}

// Release gives up the profile claim, or removes the private directory the
// browser ran in instead. It runs once the browser is stopped, because a
// running browser still holds the directory.
func (b *OwnedBrowser) Release() {
	b.lock.Release()
	b.lock = nil
	if b.DataDir != "" && b.DataDir != b.Profile {
		_ = os.RemoveAll(b.DataDir)
	}
	b.DataDir = ""
}
