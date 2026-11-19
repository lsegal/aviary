package browser

import (
	"context"
	"os/exec"

	owned "github.com/lsegal/aviary/internal/projects/process"
)

// Supervisor starts, waits on, and kills the browser subprocess. Every browser
// Aviary starts goes through one, so none of them can outlive Aviary (issue
// #773): Chrome allows one process per --user-data-dir, and a browser left
// running holds the profile against the next launch.
type Supervisor interface {
	// Start starts cmd as an owned subprocess.
	Start(cmd *exec.Cmd) error
	// Wait waits for a started subprocess to exit and stops owning it.
	Wait(cmd *exec.Cmd) error
	// Kill ends a started subprocess and everything it spawned at once. It
	// only signals, so it is safe while another goroutine is in Wait.
	Kill(cmd *exec.Cmd) error
}

// supervisor returns the supervisor to spawn this browser's process with. The
// default is Aviary's own child-process tracking: the browser runs in its own
// process group, is reaped by ReapAll and ExitAfterReaping,
// and is put under the orphan guard (a kill-on-close Job Object on Windows,
// the parent-death signal on Linux, the reaper elsewhere) the moment it starts,
// so even an exit that runs no cleanup at all takes it down.
func (c Config) supervisor() Supervisor {
	if c.Supervisor == nil {
		return owned.Supervisor{}
	}
	return c.Supervisor
}

// child is a browser process Aviary started and is waiting on.
type child struct {
	cmd        *exec.Cmd
	supervisor Supervisor
	// exited is closed once the process is gone, and waitErr is valid after it.
	exited  chan struct{}
	waitErr error
}

// startChild starts cmd through supervisor and waits on it in the background,
// so a shutdown can wait for the exit with a deadline.
func startChild(supervisor Supervisor, cmd *exec.Cmd) (*child, error) {
	if err := supervisor.Start(cmd); err != nil {
		return nil, err
	}
	c := &child{cmd: cmd, supervisor: supervisor, exited: make(chan struct{})}
	go func() {
		c.waitErr = supervisor.Wait(cmd)
		close(c.exited)
	}()
	return c, nil
}

// stop asks the browser to shut down over its DevTools WebSocket and waits for
// it to do so, because only a clean shutdown writes out the profile state it is
// still holding in memory. The process is killed only if it is still there when
// the wait runs out, or at once when there is no endpoint to ask because the
// browser never came up. A request that fails gets a shorter wait: the browser
// may already be on its way out, but nothing is coming to shut it down.
func (c *child) stop(wsURL string) {
	if c == nil {
		return
	}
	select {
	case <-c.exited:
		return
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), windowStopTimeout)
	defer cancel()
	wait := ctx.Done()
	if wsURL == "" {
		cancel()
	} else if err := closeBrowser(ctx, wsURL); err != nil {
		failed, cancelFailed := context.WithTimeout(ctx, windowCloseFailedGrace)
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
