package ngrok

import (
	"bytes"
	"errors"
	"testing"
)

// The agent found in issue #364 was started by a glorp old enough to use a
// different log level, so the match cannot depend on the flags glorp happens to
// pass today.
func TestOrphanedNgrokAgentsSelectsAbandonedGlorpTunnels(t *testing.T) {
	processes := parseProcessList(`
  72096     1 /opt/homebrew/bin/ngrok http --log=stdout --log-level=crit [::]:65026
  81002     1 ngrok http --log=stdout --log-format=json --log-level=info 127.0.0.1:65026
  81003   402 /opt/homebrew/bin/ngrok http --log=stdout --log-level=info 127.0.0.1:65027
  81004     1 /opt/homebrew/bin/ngrok http 8080
  81005     1 /opt/homebrew/bin/ngrok start --all
  81006     1 /usr/bin/sleep 60 --log=stdout http
`)
	if len(processes) != 6 {
		t.Fatalf("parsed %d processes, want 6", len(processes))
	}
	orphans := orphanedNgrokAgents(processes)
	pids := map[int]bool{}
	for _, orphan := range orphans {
		pids[orphan.pid] = true
	}
	if len(orphans) != 2 || !pids[72096] || !pids[81002] {
		t.Fatalf("selected %v, want the two abandoned glorp agents 72096 and 81002", orphans)
	}
}

// A second glorp watching another repository still owns its agent, and reaping
// it would tear down a live tunnel.
func TestOrphanedNgrokAgentsLeavesALiveGlorpsAgentAlone(t *testing.T) {
	processes := []processEntry{{pid: 4242, ppid: 402, command: "ngrok http --log=stdout --log-level=info 127.0.0.1:65026"}}
	if orphans := orphanedNgrokAgents(processes); len(orphans) != 0 {
		t.Fatalf("selected %v, want no orphans", orphans)
	}
}

func TestParseProcessListSkipsUnreadableRecords(t *testing.T) {
	processes := parseProcessList("bad record here\n  12  x ngrok http\n  13  1 ngrok http --log=stdout\n")
	if len(processes) != 1 || processes[0].pid != 13 || processes[0].ppid != 1 {
		t.Fatalf("parsed %v, want only the readable record", processes)
	}
}

func TestReapOrphanedNgrokAgentsStopsAndReportsThem(t *testing.T) {
	stubProcessSweep(t,
		func() ([]processEntry, error) {
			return []processEntry{
				{pid: 11, ppid: 1, command: "ngrok http --log=stdout --log-level=crit [::]:65026"},
				{pid: 12, ppid: 1, command: "ngrok start --all"},
				{pid: 13, ppid: 900, command: "ngrok http --log=stdout 127.0.0.1:1"},
			}, nil
		},
		nil)
	var killed []int
	terminateOrphanProcess = func(pid int) error {
		killed = append(killed, pid)
		return nil
	}
	var out bytes.Buffer

	if stopped := reapOrphanedNgrokAgents(&out); stopped != 1 {
		t.Fatalf("stopped %d agents, want 1", stopped)
	}
	if len(killed) != 1 || killed[0] != 11 {
		t.Fatalf("terminated %v, want only the orphaned agent 11", killed)
	}
	if !bytes.Contains(out.Bytes(), []byte("stopped 1 leftover ngrok agent")) {
		t.Fatalf("sweep said %q, want a report of the stopped agent", out.String())
	}
}

// A sweep is a courtesy, not a precondition: a machine whose process list
// cannot be read must still get its tunnel.
func TestReapOrphanedNgrokAgentsToleratesFailures(t *testing.T) {
	stubProcessSweep(t,
		func() ([]processEntry, error) { return nil, errors.New("ps unavailable") },
		func(int) error { return errors.New("not permitted") })
	var out bytes.Buffer
	if stopped := reapOrphanedNgrokAgents(&out); stopped != 0 {
		t.Fatalf("stopped %d agents, want 0", stopped)
	}

	listRunningProcesses = func() ([]processEntry, error) {
		return []processEntry{{pid: 11, ppid: 1, command: "ngrok http --log=stdout 127.0.0.1:1"}}, nil
	}
	if stopped := reapOrphanedNgrokAgents(&out); stopped != 0 {
		t.Fatalf("counted %d agents it could not stop, want 0", stopped)
	}
	if out.Len() != 0 {
		t.Fatalf("sweep said %q, want nothing reported", out.String())
	}
}

func stubProcessSweep(t *testing.T, list func() ([]processEntry, error), terminate func(int) error) {
	t.Helper()
	previousList, previousTerminate := listRunningProcesses, terminateOrphanProcess
	t.Cleanup(func() { listRunningProcesses, terminateOrphanProcess = previousList, previousTerminate })
	if list != nil {
		listRunningProcesses = list
	}
	if terminate != nil {
		terminateOrphanProcess = terminate
	}
}

// The npm package's bin is the ngrok agent itself rather than a Node shim, so
// an agent glorp started through npx (issue #498) leaves the same argv behind
// as an installed one and is reaped by the same match. Node is what npx runs,
// not what the agent ends up being, so a stray npx must not be swept up with
// it.
func TestOrphanedNgrokAgentsMatchesAnNpxLaunchedAgent(t *testing.T) {
	processes := parseProcessList(`
  90100     1 /home/u/.npm/_npx/094a17e86d/node_modules/ngrok/bin/ngrok http --log=stdout --log-format=json --log-level=info 127.0.0.1:65026
  90101     1 /usr/bin/node /home/u/.npm/_npx/094a17e86d/node_modules/.bin/npx --yes ngrok http --log=stdout
`)
	orphans := orphanedNgrokAgents(processes)
	if len(orphans) != 1 || orphans[0].pid != 90100 {
		t.Fatalf("selected %v, want only the npx-launched agent 90100", orphans)
	}
}
