package projects

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeCheckpointFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// checkpointRepo makes a bare remote with a main branch and a clone working
// on fix/issue-7-x, pushed with upstream tracking as gh-fix leaves it.
func checkpointRepo(t *testing.T) (remote, checkout string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	remote = filepath.Join(root, "remote.git")
	checkout = filepath.Join(root, "checkout")
	gitIn(t, root, "init", "--quiet", "--bare", "--initial-branch=main", remote)
	gitIn(t, root, "init", "--quiet", "--initial-branch=main", checkout)
	configureCheckpointIdentity(t, checkout)
	gitIn(t, checkout, "remote", "add", "origin", remote)
	writeCheckpointFile(t, filepath.Join(checkout, "a.txt"), "one\n")
	gitIn(t, checkout, "add", ".")
	gitIn(t, checkout, "commit", "--quiet", "-m", "initial")
	gitIn(t, checkout, "push", "--quiet", "-u", "origin", "main")
	gitIn(t, checkout, "remote", "set-head", "origin", "main")
	gitIn(t, checkout, "checkout", "--quiet", "-b", "fix/issue-7-x")
	gitIn(t, checkout, "push", "--quiet", "-u", "origin", "fix/issue-7-x")
	return remote, checkout
}

func configureCheckpointIdentity(t *testing.T, dir string) {
	t.Helper()
	gitIn(t, dir, "config", "user.name", "t")
	gitIn(t, dir, "config", "user.email", "t@example.com")
	gitIn(t, dir, "config", "commit.gpgsign", "false")
	gitIn(t, dir, "config", "core.hooksPath", os.DevNull)
}

func checkpointIssue7(t *testing.T, checkout string) core.CheckpointResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return checkpointCheckout(ctx, runCheckpointGit, checkpointRun{Target: "o/r", Number: 7, Checkout: checkout})
}

func TestCheckpointCommitsAndPushesUnpushedWork(t *testing.T) {
	remote, checkout := checkpointRepo(t)
	writeCheckpointFile(t, filepath.Join(checkout, "a.txt"), "two\n")
	gitIn(t, checkout, "commit", "--quiet", "-am", "committed but not pushed")
	writeCheckpointFile(t, filepath.Join(checkout, "a.txt"), "three\n")
	writeCheckpointFile(t, filepath.Join(checkout, "new.txt"), "untracked\n")

	result := checkpointIssue7(t, checkout)
	if result.Outcome != checkpointPushed || result.Branch != "fix/issue-7-x" {
		t.Fatalf("checkpoint = %+v, want pushed fix/issue-7-x", result)
	}
	if got := gitIn(t, checkout, "status", "--porcelain"); got != "" {
		t.Fatalf("checkout still dirty after checkpoint: %q", got)
	}
	if got := gitIn(t, remote, "log", "-1", "--format=%s", "fix/issue-7-x"); got != checkpointMessage(7) {
		t.Fatalf("remote branch tip = %q, want the shutdown checkpoint", got)
	}
	if got := gitIn(t, remote, "show", "fix/issue-7-x:new.txt"); got != "untracked" {
		t.Fatalf("remote new.txt = %q", got)
	}
	if got := gitIn(t, remote, "log", "-1", "--format=%s", "main"); got != "initial" {
		t.Fatalf("checkpoint moved main to %q", got)
	}

	// A second shutdown with nothing new is a no-op (Requirement 7).
	tip := gitIn(t, checkout, "rev-parse", "HEAD")
	if again := checkpointIssue7(t, checkout); again.Outcome != checkpointNothing {
		t.Fatalf("second checkpoint = %+v, want nothing to push", again)
	}
	if got := gitIn(t, checkout, "rev-parse", "HEAD"); got != tip {
		t.Fatal("second checkpoint created another commit")
	}
}

func TestCheckpointPushesCommittedWorkWithoutNewCommit(t *testing.T) {
	remote, checkout := checkpointRepo(t)
	writeCheckpointFile(t, filepath.Join(checkout, "a.txt"), "two\n")
	gitIn(t, checkout, "commit", "--quiet", "-am", "agent commit")
	if result := checkpointIssue7(t, checkout); result.Outcome != checkpointPushed {
		t.Fatalf("checkpoint = %+v, want pushed", result)
	}
	if got := gitIn(t, remote, "log", "-1", "--format=%s", "fix/issue-7-x"); got != "agent commit" {
		t.Fatalf("remote tip = %q, want the agent's own commit and no checkpoint commit", got)
	}
}

func TestCheckpointLeavesCleanPushedCheckoutAlone(t *testing.T) {
	_, checkout := checkpointRepo(t)
	tip := gitIn(t, checkout, "rev-parse", "HEAD")
	if result := checkpointIssue7(t, checkout); result.Outcome != checkpointNothing {
		t.Fatalf("checkpoint = %+v, want nothing to push", result)
	}
	if got := gitIn(t, checkout, "rev-parse", "HEAD"); got != tip {
		t.Fatal("clean checkout got a checkpoint commit")
	}
}

func TestCheckpointUnreachableRemoteLeavesTreeUntouched(t *testing.T) {
	_, checkout := checkpointRepo(t)
	gitIn(t, checkout, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	writeCheckpointFile(t, filepath.Join(checkout, "a.txt"), "dirty\n")
	tip := gitIn(t, checkout, "rev-parse", "HEAD")
	status := gitIn(t, checkout, "status", "--porcelain")

	result := checkpointIssue7(t, checkout)
	if result.Outcome != checkpointFailed || !strings.Contains(result.Detail, "unreachable") {
		t.Fatalf("checkpoint = %+v, want failed as unreachable", result)
	}
	if got := gitIn(t, checkout, "rev-parse", "HEAD"); got != tip {
		t.Fatal("failed checkpoint left a commit behind")
	}
	if got := gitIn(t, checkout, "status", "--porcelain"); got != status {
		t.Fatalf("failed checkpoint changed the tree: %q, want %q", got, status)
	}
}

func TestCheckpointNeverForcePushesOverMovedBranch(t *testing.T) {
	remote, checkout := checkpointRepo(t)
	other := filepath.Join(t.TempDir(), "other")
	gitIn(t, filepath.Dir(other), "clone", "--quiet", "--branch", "fix/issue-7-x", remote, other)
	configureCheckpointIdentity(t, other)
	writeCheckpointFile(t, filepath.Join(other, "b.txt"), "elsewhere\n")
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "--quiet", "-m", "pushed elsewhere")
	gitIn(t, other, "push", "--quiet")
	moved := gitIn(t, remote, "rev-parse", "fix/issue-7-x")

	writeCheckpointFile(t, filepath.Join(checkout, "a.txt"), "local\n")
	tip := gitIn(t, checkout, "rev-parse", "HEAD")
	result := checkpointIssue7(t, checkout)
	if result.Outcome != checkpointFailed || !strings.Contains(result.Detail, "fast-forward") {
		t.Fatalf("checkpoint = %+v, want a non-fast-forward failure", result)
	}
	if got := gitIn(t, remote, "rev-parse", "fix/issue-7-x"); got != moved {
		t.Fatal("checkpoint overwrote the remote branch")
	}
	if got := gitIn(t, checkout, "rev-parse", "HEAD"); got != tip {
		t.Fatal("failed checkpoint left a commit behind")
	}
}

func TestCheckpointRefusesBaseBranch(t *testing.T) {
	remote, checkout := checkpointRepo(t)
	gitIn(t, checkout, "checkout", "--quiet", "main")
	writeCheckpointFile(t, filepath.Join(checkout, "a.txt"), "on main\n")
	before := gitIn(t, remote, "rev-parse", "main")
	result := checkpointIssue7(t, checkout)
	if result.Outcome != checkpointFailed || !strings.Contains(result.Detail, "base branch") {
		t.Fatalf("checkpoint = %+v, want refusal to push the base branch", result)
	}
	if got := gitIn(t, remote, "rev-parse", "main"); got != before {
		t.Fatal("checkpoint pushed to main")
	}
}

func TestCheckpointRefusesBranchWithoutOwnUpstream(t *testing.T) {
	_, checkout := checkpointRepo(t)
	gitIn(t, checkout, "checkout", "--quiet", "-b", "fix/issue-7-local")
	writeCheckpointFile(t, filepath.Join(checkout, "a.txt"), "x\n")
	if result := checkpointIssue7(t, checkout); result.Outcome != checkpointFailed || !strings.Contains(result.Detail, "no upstream") {
		t.Fatalf("checkpoint = %+v, want no-upstream failure", result)
	}
	gitIn(t, checkout, "branch", "--quiet", "--set-upstream-to=origin/main")
	if result := checkpointIssue7(t, checkout); result.Outcome != checkpointFailed || !strings.Contains(result.Detail, "tracks origin/main") {
		t.Fatalf("checkpoint = %+v, want refusal to push to another branch", result)
	}
}

func TestCheckpointRefusesDetachedHead(t *testing.T) {
	_, checkout := checkpointRepo(t)
	gitIn(t, checkout, "checkout", "--quiet", "--detach")
	writeCheckpointFile(t, filepath.Join(checkout, "a.txt"), "x\n")
	if result := checkpointIssue7(t, checkout); result.Outcome != checkpointFailed || !strings.Contains(result.Detail, "detached") {
		t.Fatalf("checkpoint = %+v, want detached-HEAD failure", result)
	}
}

// In the cross-repo flow (issue #712) the branch tracks the fork remote, and
// the checkpoint pushes there and nowhere else.
func TestCheckpointPushesForkRunToFork(t *testing.T) {
	origin, checkout := checkpointRepo(t)
	fork := filepath.Join(t.TempDir(), "fork.git")
	gitIn(t, filepath.Dir(fork), "init", "--quiet", "--bare", fork)
	gitIn(t, checkout, "remote", "add", "fork", fork)
	gitIn(t, checkout, "checkout", "--quiet", "-b", "fix/issue-7-fork")
	gitIn(t, checkout, "push", "--quiet", "-u", "fork", "fix/issue-7-fork")
	writeCheckpointFile(t, filepath.Join(checkout, "a.txt"), "fork work\n")

	if result := checkpointIssue7(t, checkout); result.Outcome != checkpointPushed {
		t.Fatalf("checkpoint = %+v, want pushed", result)
	}
	if got := gitIn(t, fork, "log", "-1", "--format=%s", "fix/issue-7-fork"); got != checkpointMessage(7) {
		t.Fatalf("fork tip = %q", got)
	}
	if got := gitIn(t, origin, "branch", "--list", "fix/issue-7-fork"); got != "" {
		t.Fatalf("checkpoint pushed the fork branch to origin: %q", got)
	}
}

func TestCheckpointWithoutCheckoutIsNoOp(t *testing.T) {
	result := checkpointCheckout(context.Background(), runCheckpointGit, checkpointRun{Target: "o/r", Number: 1})
	if result.Outcome != checkpointNothing {
		t.Fatalf("checkpoint = %+v, want nothing to push", result)
	}
}

// A run that hangs past the phase budget is reported as timed out and the
// phase still returns on time (Requirement 4).
func TestCheckpointAllBoundsTheWholePhase(t *testing.T) {
	dir := t.TempDir()
	block := make(chan struct{})
	defer close(block)
	hang := func(ctx context.Context, d string, args ...string) (string, error) {
		<-block // ignores cancellation, like a git that will not die
		return "", context.Canceled
	}
	runs := []checkpointRun{{Target: "o/r", Number: 1, Checkout: dir}, {Target: "o/r", Number: 2}}
	start := time.Now()
	results := checkpointAll(runs, hang, time.Hour, 200*time.Millisecond, nil)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("phase took %s, want it bounded by its budget", elapsed)
	}
	if results[0].Outcome != checkpointTimedOut {
		t.Fatalf("hung run = %+v, want timed out", results[0])
	}
	if results[1].Outcome != checkpointNothing {
		t.Fatalf("run without a checkout = %+v, want nothing to push", results[1])
	}
}

func TestCheckpointRunBudgetReportsTimeout(t *testing.T) {
	dir := t.TempDir()
	slow := func(ctx context.Context, d string, args ...string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}
	results := checkpointAll([]checkpointRun{{Target: "o/r", Number: 1, Checkout: dir}}, slow, 50*time.Millisecond, time.Minute, nil)
	if results[0].Outcome != checkpointTimedOut {
		t.Fatalf("slow run = %+v, want timed out", results[0])
	}
}

// The restart button and the shutdown signals share one checkpointer, so a
// second caller gets the first one's results instead of a second checkpoint.
func TestShutdownCheckpointerRunsOnce(t *testing.T) {
	agents := &activeAgents{}
	agents.add(Issue{Target: "o/r", Number: 4}, "")
	var logged []string
	s := newShutdownCheckpointer(agents, func(format string, args ...interface{}) { logged = append(logged, format) })
	first := s.Checkpoint()
	second := s.Checkpoint()
	if len(first) != 1 || first[0].Number != 4 || first[0].Outcome != checkpointNothing {
		t.Fatalf("checkpoint = %+v", first)
	}
	if len(second) != 1 || second[0] != first[0] {
		t.Fatalf("second checkpoint = %+v, want the first results", second)
	}
	if len(logged) != 3 {
		t.Fatalf("logged %d lines, want a header, one outcome, and the handoff: %q", len(logged), logged)
	}
}

func TestShutdownCheckpointerWithoutRunsLogsNothing(t *testing.T) {
	logged := 0
	s := newShutdownCheckpointer(&activeAgents{}, func(string, ...interface{}) { logged++ })
	if results := s.Checkpoint(); len(results) != 0 || logged != 0 {
		t.Fatalf("results = %+v, logged = %d", results, logged)
	}
}

// activeAgents follows the checkout an agent announces and forgets a run once
// it ends, so the checkpoint only sees live runs.
func TestActiveAgentsTracksRunsAndCheckouts(t *testing.T) {
	agents := &activeAgents{}
	run := agents.add(Issue{Target: "o/r", Number: 9}, "")
	agents.setCheckout(run, "/tmp/clone")
	other := agents.add(Issue{Target: "o/r", Number: 2}, "/tmp/other")
	agents.remove(other)
	runs := agents.pauseAll()
	if len(runs) != 1 || runs[0].Number != 9 || runs[0].Checkout != "/tmp/clone" {
		t.Fatalf("runs = %+v", runs)
	}
	var none *activeAgents
	if none.add(Issue{}, "") != nil || none.pauseAll() != nil {
		t.Fatal("nil tracker recorded a run")
	}
}

// A dispatched agent is tracked for the shutdown checkpoint while it runs,
// following the checkout it announces, and forgotten once it exits.
func TestRunnerTracksActiveAgentForCheckpoint(t *testing.T) {
	checkout := t.TempDir()
	contract := agentContract{Definition: builtinDefinition(t, "codex"), Repo: "o/r", Number: 7}
	runner, _ := contract.runner(t, fakeAgentRun{Checkout: checkout})
	runner.Active = &activeAgents{}
	var seen []checkpointRun
	if err := runner.RunSession(context.Background(), contract.issue(), AgentSession{Agent: "codex"}, func(update AgentSession) {
		if update.CheckoutDirectory == "" {
			return
		}
		runner.Active.mu.Lock()
		for run := range runner.Active.runs {
			seen = append(seen, checkpointRun{Number: run.issue.Number, Checkout: run.checkout})
		}
		runner.Active.mu.Unlock()
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if len(seen) != 1 || seen[0].Number != 7 || seen[0].Checkout == "" {
		t.Fatalf("tracked runs while running = %+v, want issue #7 in its checkout", seen)
	}
	if runs := runner.Active.pauseAll(); len(runs) != 0 {
		t.Fatalf("tracked runs after exit = %+v, want none", runs)
	}
}

func (a *activeAgents) trackedNumbers() []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	var numbers []int
	for run := range a.runs {
		numbers = append(numbers, run.issue.Number)
	}
	return numbers
}

// A run waiting out a scheduled wakeup has no agent process but still has a
// checkout, so the shutdown checkpoint covers it too.
func TestGlorpTracksRunWaitingOnWakeupForCheckpoint(t *testing.T) {
	runner := &wakeupRunner{
		sessions: make(chan AgentSession, 4),
		wakeups:  []*scheduledWakeup{{Delay: time.Hour, Prompt: "check CI", Reason: "watching CI run"}},
	}
	h := startWakeupHarness(t, runner, time.Hour)
	waitForSession(t, runner.sessions)
	waitForJobStatus(t, h.reporter, 7, "waiting")
	deadline := time.Now().Add(5 * time.Second)
	for len(h.w.Active.trackedNumbers()) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := h.w.Active.trackedNumbers(); len(got) != 1 || got[0] != 7 {
		t.Fatalf("tracked runs while waiting = %v, want issue #7", got)
	}
	if err := h.w.handleJobAction(h.ctx, jobAction{Action: "stop", Target: "o/r", Number: 7}); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitForJobStatus(t, h.reporter, 7, "failed")
	if got := h.w.Active.trackedNumbers(); len(got) != 0 {
		t.Fatalf("tracked runs after stop = %v, want none", got)
	}
}
