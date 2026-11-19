//go:build !linux && !windows

package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// macOS and the BSDs offer no equivalent of Linux's parent-death signal or
// Windows' job objects, so the kernel cannot take glorp's subprocesses down with
// it. A supervised reaper stands in for that (issue #266): glorp holds the write
// end of a pipe to a small shell process and reports the process group of every
// child it owns. The kernel closes that pipe when glorp's process object is
// destroyed — including on a SIGKILL that runs no userspace cleanup at all — and
// the reaper then terminates every group still recorded.
//
// Only groups glorp still owns are recorded, so a normal shutdown, which already
// reaps its own children, leaves the reaper with nothing to do and no chance of
// signalling a process group whose id has since been reused.
const orphanReaperScript = `
live=""
while IFS=' ' read -r op pgid; do
	case "$op" in
	+) live="$live $pgid" ;;
	-)
		remaining=""
		for pid in $live; do
			[ "$pid" = "$pgid" ] || remaining="$remaining $pid"
		done
		live=$remaining
		;;
	esac
done
[ -n "$live" ] || exit 0
for pid in $live; do kill -TERM "-$pid" 2>/dev/null; done
sleep 1
for pid in $live; do kill -KILL "-$pid" 2>/dev/null; done
`

// orphanReaper is glorp's single connection to its reaper process. The pipe is
// deliberately never closed while glorp runs: its closing is the signal the
// reaper acts on.
var orphanReaper struct {
	mu    sync.Mutex
	once  sync.Once
	pipe  io.Writer
	owned map[*exec.Cmd]int
}

// startOrphanReaper starts the reaper process and returns the pipe glorp reports
// owned process groups on, along with the reaper itself.
func startOrphanReaper() (io.WriteCloser, *exec.Cmd, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = reader.Close() }()
	cmd := exec.Command("/bin/sh", "-c", orphanReaperScript)
	cmd.Stdin = reader
	// The reaper needs its own process group: it must survive whatever takes
	// glorp down long enough to clean up after it.
	isolateProcessTree(cmd)
	if err := spawner.start(cmd); err != nil {
		_ = writer.Close()
		return nil, nil, err
	}
	// Nothing consumes the reaper's exit status, so reap it in the background to
	// keep it from lingering as a zombie if it dies early.
	go func() { _ = cmd.Wait() }()
	return writer, cmd, nil
}

// orphanReaperPipe returns the reaper's pipe, starting the reaper on first use.
// It reports nil when the reaper could not be started; glorp's own cleanup paths
// still cover every exit it can observe.
func orphanReaperPipe() io.Writer {
	orphanReaper.once.Do(func() {
		pipe, _, err := startOrphanReaper()
		if err != nil {
			return
		}
		orphanReaper.pipe = pipe
	})
	return orphanReaper.pipe
}

// guardOrphanedProcess has nothing to do before the child starts: its process
// group only exists once it is running.
func guardOrphanedProcess(*exec.Cmd) {}

// adoptOrphanedProcess records a freshly started child's process group with the
// reaper. Children that share glorp's own process group are skipped: they
// already receive the terminal's signals, and killing that group after glorp is
// gone could reach well beyond glorp's own subprocesses. Nothing here can leave
// a child unusable, so no failure is worth reporting: an unrecorded child is
// still cleaned up on every exit glorp can observe.
func adoptOrphanedProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil || pgid != cmd.Process.Pid {
		return nil
	}
	orphanReaper.mu.Lock()
	defer orphanReaper.mu.Unlock()
	if orphanReaper.owned == nil {
		orphanReaper.owned = map[*exec.Cmd]int{}
	}
	orphanReaper.owned[cmd] = pgid
	recordOrphanedGroup("+", pgid)
	return nil
}

// releaseOrphanedProcess tells the reaper glorp no longer owns a child, so a
// later reap cannot signal a process group id that has been reused since. The
// group is remembered from adoption because a child that has already exited no
// longer has one to look up.
func releaseOrphanedProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	orphanReaper.mu.Lock()
	defer orphanReaper.mu.Unlock()
	pgid, ok := orphanReaper.owned[cmd]
	if !ok {
		return
	}
	delete(orphanReaper.owned, cmd)
	recordOrphanedGroup("-", pgid)
}

// recordOrphanedGroup writes one record to the reaper. Callers hold the lock so
// records cannot interleave in the pipe.
func recordOrphanedGroup(op string, pgid int) {
	pipe := orphanReaperPipe()
	if pipe == nil {
		return
	}
	_, _ = fmt.Fprintf(pipe, "%s %d\n", op, pgid)
}
