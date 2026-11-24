package cliagent

import (
	"io"
	"os/exec"
	"time"

	"github.com/lsegal/aviary/internal/process"
)

// turnWaitDelay is how long a turn's output may stay open after its CLI
// exits, or is killed, before the turn stops waiting on it.
var turnWaitDelay = 5 * time.Second

// startTurn starts one turn's CLI process and returns its standard output
// and a wait function, to call once that output is read to its end.
//
// The CLI runs in its own process group. A background process it started
// that keeps the output open once the CLI exits would otherwise hold the
// turn forever: the output ends turnWaitDelay after the exit instead, and
// process.Wait kills what is left (#1122).
func startTurn(cmd *exec.Cmd) (io.Reader, func() error, error) {
	reader, writer := io.Pipe()
	cmd.Stdout = writer
	cmd.Cancel = func() error { return process.Kill(cmd) }
	cmd.WaitDelay = turnWaitDelay
	if err := process.Start(cmd); err != nil {
		_ = writer.Close()
		return nil, nil, err
	}
	done := make(chan error, 1)
	go func() {
		err := process.Wait(cmd)
		_ = writer.Close()
		done <- err
	}()
	return reader, func() error { return <-done }, nil
}
