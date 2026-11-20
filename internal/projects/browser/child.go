package browser

import (
	"context"
	"os/exec"

	aviarybrowser "github.com/lsegal/aviary/internal/browser"

	owned "github.com/lsegal/aviary/internal/projects/process"
)

// Supervisor starts, waits on, and kills the browser subprocess. Every browser
// Aviary starts goes through one, so none of them can outlive Aviary (issue
// #773): Chrome allows one process per --user-data-dir, and a browser left
// running holds the profile against the next launch.
type Supervisor = aviarybrowser.Supervisor

// supervisor returns the supervisor to spawn this browser's process with. The
// default is Aviary's own child-process tracking: the browser runs in its own
// process group, is reaped by ReapAll and ExitAfterReaping,
// and is put under the orphan guard (a kill-on-close Job Object on Windows,
// the parent-death signal on Linux, the reaper elsewhere) the moment it starts,
// so even an exit that runs no cleanup at all takes it down.
func (c Config) supervisor() Supervisor {
	if c.Supervisor == nil {
		return ownedSupervisor{}
	}
	return c.Supervisor
}

// ownedSupervisor runs the browser as one of Aviary's tracked subprocesses.
type ownedSupervisor struct{}

func (ownedSupervisor) Start(cmd *exec.Cmd) error { return owned.Start(cmd) }
func (ownedSupervisor) Wait(cmd *exec.Cmd) error  { return owned.Wait(cmd) }
func (ownedSupervisor) Kill(cmd *exec.Cmd) error  { return owned.Kill(cmd) }

// shutdownPolicy is how a browser this package started is stopped. The close
// request is looked up when it is sent, so tests can stand in for it.
func shutdownPolicy() aviarybrowser.ShutdownPolicy {
	return aviarybrowser.ShutdownPolicy{
		Timeout:     windowStopTimeout,
		FailedGrace: windowCloseFailedGrace,
		Close:       func(ctx context.Context, wsURL string) error { return closeBrowser(ctx, wsURL) },
	}
}
