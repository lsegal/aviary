package ngrok

import (
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// glorp's platform orphan guards (issues #260, #264, #266) cover every exit
// path the kernel can act on, but an ngrok agent was still found running twelve
// days after the run that started it (issue #364): the guard belongs to the
// glorp build that started the child, so an agent left behind by an older
// build, or by a reaper that was killed along with its parent, has nobody left
// to clean it up. Such an agent is not idle waste — it owns the local ngrok API
// port and consumes one of the account's simultaneous agent sessions, and a
// couple of them make every later run fail with ERR_NGROK_108 — so glorp sweeps
// up its own leftovers before starting a tunnel of its own.

// processEntry is the subset of a running process glorp needs to recognize a
// leftover agent of its own.
type processEntry struct {
	pid     int
	ppid    int
	command string
}

// listRunningProcesses and terminateOrphanProcess are variables so tests can
// drive the sweep without depending on what happens to be running.
var (
	listRunningProcesses   = platformRunningProcesses
	terminateOrphanProcess = platformTerminateProcess
)

// reapOrphanedNgrokAgents stops every ngrok agent left behind by an earlier
// glorp run and reports how many were stopped. Failing to sweep is never worth
// failing a startup over: the tunnel that follows either works or reports its
// own error.
func reapOrphanedNgrokAgents(out io.Writer) int {
	processes, err := listRunningProcesses()
	if err != nil {
		return 0
	}
	stopped := 0
	for _, process := range orphanedNgrokAgents(processes) {
		if err := terminateOrphanProcess(process.pid); err == nil {
			stopped++
		}
	}
	if stopped > 0 && out != nil {
		fmt.Fprintf(out, "stopped %d leftover ngrok agent(s) from an earlier glorp run\n", stopped)
	}
	return stopped
}

// orphanedNgrokAgents picks out the agents glorp started and no longer owns.
// Only reparented processes qualify: an agent still owned by a live glorp — a
// second instance watching another repository — has that glorp as its parent
// and must be left alone.
func orphanedNgrokAgents(processes []processEntry) []processEntry {
	orphans := make([]processEntry, 0, len(processes))
	for _, process := range processes {
		if process.pid <= 1 || process.ppid != 1 {
			continue
		}
		if !glorpNgrokCommand(process.command) {
			continue
		}
		orphans = append(orphans, process)
	}
	return orphans
}

// glorpNgrokCommand reports whether a command line is an ngrok agent started by
// glorp. glorp is the reason an agent logs to standard output: the interactive
// dashboard is what ngrok does by default, so `ngrok http --log=stdout` is the
// signature of a glorp tunnel rather than one a user started by hand. The log
// level is not part of the match, because it has changed across glorp releases
// and the agents worth reaping are precisely the ones from older ones.
func glorpNgrokCommand(command string) bool {
	fields := strings.Fields(command)
	if len(fields) < 2 || !ngrokExecutable(fields[0]) {
		return false
	}
	http, stdout := false, false
	for _, field := range fields[1:] {
		switch {
		case field == "http":
			http = true
		case field == "--log=stdout" || field == "--log stdout":
			stdout = true
		}
	}
	return http && stdout
}

// ngrokExecutable reports whether a command's argv[0] names the ngrok binary,
// whatever path or extension it was invoked through.
func ngrokExecutable(argv0 string) bool {
	name := filepath.Base(strings.ReplaceAll(argv0, `\`, "/"))
	return strings.EqualFold(strings.TrimSuffix(name, ".exe"), "ngrok")
}

// parseProcessList reads the `pid ppid command` records glorp asks the platform
// process lister for. Lines it cannot read are skipped rather than failing the
// sweep: one unreadable record must not hide the orphans in the rest.
func parseProcessList(output string) []processEntry {
	var processes []processEntry
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		processes = append(processes, processEntry{pid: pid, ppid: ppid, command: strings.Join(fields[2:], " ")})
	}
	return processes
}
