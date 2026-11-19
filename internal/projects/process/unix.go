//go:build !windows

package process

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/mattn/go-isatty"
)

// isolateProcessTree puts the child in its own process group so glorp can
// signal the child and everything it spawns as one unit. A child that inherits
// glorp's terminal keeps sharing glorp's group: a background group reading the
// terminal is stopped with SIGTTIN, and such a child already receives the
// terminal's own interrupt and hangup signals.
func isolateProcessTree(cmd *exec.Cmd) {
	if file, ok := cmd.Stdin.(*os.File); ok && file != nil && isatty.IsTerminal(file.Fd()) {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// signalProcessTree signals the child's whole process group, falling back to
// the child alone when it never got a group of its own. It reports
// os.ErrProcessDone once nothing in the tree is left to signal.
func signalProcessTree(cmd *exec.Cmd, signal processSignal) error {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return os.ErrProcessDone
	}
	target := cmd.Process.Pid
	// Never signal glorp's own process group: a child that was not isolated
	// shares it, and killing the group would take the parent down too.
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil && pgid != syscall.Getpgrp() {
		target = -pgid
	}
	if err := syscall.Kill(target, unixSignal(signal)); err != nil {
		return os.ErrProcessDone
	}
	return nil
}

func unixSignal(signal processSignal) syscall.Signal {
	switch signal {
	case terminateSignal:
		return syscall.SIGTERM
	case killSignal:
		return syscall.SIGKILL
	case pauseSignal:
		return syscall.SIGSTOP
	case resumeSignal:
		return syscall.SIGCONT
	default:
		return syscall.Signal(0)
	}
}
