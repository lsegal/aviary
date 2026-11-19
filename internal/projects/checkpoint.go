package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/projects/process"
)

// The shutdown checkpoint is bounded so a slow or unreachable
// remote can never hold a restart or exit hostage: each run gets
// checkpointRunBudget to commit and push, and the whole phase ends after
// checkpointPhaseBudget whatever is still outstanding.
const (
	checkpointRunBudget   = 45 * time.Second
	checkpointPhaseBudget = 60 * time.Second
)

// The outcomes a run's shutdown checkpoint can report.
const (
	checkpointPushed   = "pushed"
	checkpointNothing  = "nothing to push"
	checkpointFailed   = "failed"
	checkpointTimedOut = "timed out"
	// checkpointPending is a run whose checkpoint is still going, as the
	// restart reports it while the phase runs.
	checkpointPending = "pending"
)

// checkpointGitWaitDelay bounds how long a checkpoint git command that was
// cancelled at its deadline may keep its output pipes open, such as through a
// credential helper or ssh it started, before Wait gives up on them. Without
// it a hung push held its run past the run budget.
const checkpointGitWaitDelay = 2 * time.Second

// activeAgents tracks the agent subprocesses this instance is running and the
// checkout each one works in, so the shutdown checkpoint knows what to pause
// and commit. It is shared by pointer across copies of CommandRunner, and a
// nil tracker records nothing.
type activeAgents struct {
	mu     sync.Mutex
	runs   map[*activeAgent]struct{}
	paused bool
}

type activeAgent struct {
	issue    Issue
	checkout string
	cmd      *exec.Cmd
}

// add records a run that is about to start its agent in checkout, which may
// still be empty when the agent has not announced one yet.
func (a *activeAgents) add(issue Issue, checkout string) *activeAgent {
	if a == nil {
		return nil
	}
	run := &activeAgent{issue: issue, checkout: checkout}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runs == nil {
		a.runs = map[*activeAgent]struct{}{}
	}
	a.runs[run] = struct{}{}
	return run
}

// started records the run's agent process once it is running. An agent that
// starts while the checkpoint has everything paused is paused at once, so no
// new work begins during shutdown.
func (a *activeAgents) started(run *activeAgent, cmd *exec.Cmd) {
	if a == nil || run == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	run.cmd = cmd
	if a.paused {
		_ = process.Pause(cmd)
	}
}

// setCheckout records the checkout directory the agent announced.
func (a *activeAgents) setCheckout(run *activeAgent, checkout string) {
	if a == nil || run == nil || checkout == "" {
		return
	}
	a.mu.Lock()
	run.checkout = checkout
	a.mu.Unlock()
}

func (a *activeAgents) remove(run *activeAgent) {
	if a == nil || run == nil {
		return
	}
	a.mu.Lock()
	delete(a.runs, run)
	a.mu.Unlock()
}

// pauseAll suspends every running agent so its checkout stops changing, and
// keeps any agent started afterwards paused too. Agents stay paused until
// aviary terminates them; the checkpoint is only taken on the way out. It
// reports each run with the checkout to commit, in issue order.
func (a *activeAgents) pauseAll() []checkpointRun {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.paused = true
	runs := make([]checkpointRun, 0, len(a.runs))
	for run := range a.runs {
		if run.cmd != nil {
			_ = process.Pause(run.cmd)
		}
		// A scheduled run has no issue or pull request to push its work to,
		// and is not resumed after the restart.
		if run.issue.ScheduleID != "" {
			continue
		}
		runs = append(runs, checkpointRun{Target: run.issue.Target, Number: run.issue.Number, Checkout: run.checkout})
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].Target != runs[j].Target {
			return runs[i].Target < runs[j].Target
		}
		return runs[i].Number < runs[j].Number
	})
	return runs
}

// inProgressMarkers are the git-dir entries an interrupted merge, rebase,
// cherry-pick, or revert leaves behind; a checkout holding one cannot take a
// checkpoint commit.
var inProgressMarkers = []struct{ path, name string }{
	{"MERGE_HEAD", "merge"},
	{"CHERRY_PICK_HEAD", "cherry-pick"},
	{"REVERT_HEAD", "revert"},
	{"rebase-merge", "rebase"},
	{"rebase-apply", "rebase"},
}

// checkpointRun is one active run the shutdown checkpoint commits and pushes.
type checkpointRun struct {
	Target   string
	Number   int
	Checkout string
}

// shutdownCheckpointer runs the shutdown checkpoint once per process. The
// restart button and the shutdown signals both go through it, so whichever
// asks first does the work and a later caller gets the same results.
type shutdownCheckpointer struct {
	agents  *activeAgents
	logMu   sync.Mutex
	logf    func(string, ...interface{})
	git     checkpointGit
	run     time.Duration
	overall time.Duration

	once    sync.Once
	results []core.CheckpointResult

	progressMu sync.Mutex
	progress   []core.CheckpointResult
}

func newShutdownCheckpointer(agents *activeAgents, logf func(string, ...interface{})) *shutdownCheckpointer {
	return &shutdownCheckpointer{agents: agents, logf: logf, git: runCheckpointGit, run: checkpointRunBudget, overall: checkpointPhaseBudget}
}

// SetLog sends the checkpoint's outcomes to logf, such as the run's own log
// once it exists.
func (s *shutdownCheckpointer) SetLog(logf func(string, ...interface{})) {
	s.logMu.Lock()
	s.logf = logf
	s.logMu.Unlock()
}

func (s *shutdownCheckpointer) log(format string, args ...interface{}) {
	s.logMu.Lock()
	logf := s.logf
	s.logMu.Unlock()
	logf(format, args...)
}

// Checkpoint pauses every active agent, then commits and pushes each one's
// checkout in parallel within the run and phase budgets, logging each run's
// outcome as soon as it has one. It always returns, whatever the remotes do,
// and the caller then stops the paused agents.
func (s *shutdownCheckpointer) Checkpoint() []core.CheckpointResult {
	s.once.Do(func() {
		runs := s.agents.pauseAll()
		if len(runs) == 0 {
			return
		}
		pending := make([]core.CheckpointResult, len(runs))
		for i, run := range runs {
			pending[i] = core.CheckpointResult{Target: run.Target, Number: run.Number, Outcome: checkpointPending}
		}
		s.progressMu.Lock()
		s.progress = pending
		s.progressMu.Unlock()
		s.log("shutdown checkpoint: pushing the work of %d active run(s) before stopping (up to %s)", len(runs), s.overall)
		s.results = checkpointAll(runs, s.git, s.run, s.overall, s.report)
		s.log("shutdown checkpoint: finished; stopping the agents")
	})
	return s.results
}

// report records and logs one run's outcome as it arrives, so a restart
// that takes a while shows which runs are done and which are still going.
func (s *shutdownCheckpointer) report(i int, result core.CheckpointResult) {
	s.progressMu.Lock()
	if i < len(s.progress) {
		s.progress[i] = result
	}
	s.progressMu.Unlock()
	if result.Detail != "" {
		s.log("shutdown checkpoint: %s#%d %s: %s", result.Target, result.Number, result.Outcome, result.Detail)
	} else {
		s.log("shutdown checkpoint: %s#%d %s", result.Target, result.Number, result.Outcome)
	}
}

// Progress reports each run's checkpoint as it stands, with runs still going
// reported as pending. It is empty before the checkpoint starts.
func (s *shutdownCheckpointer) Progress() []core.CheckpointResult {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	return append([]core.CheckpointResult(nil), s.progress...)
}

// checkpointAll checkpoints every run concurrently, handing each outcome to
// report, when given, as soon as it is known. A run still going when the
// overall budget ends is reported as timed out and left behind; its git
// commands are owned subprocesses, so aviary's exit reaps them.
func checkpointAll(runs []checkpointRun, git checkpointGit, perRun, overall time.Duration, report func(int, core.CheckpointResult)) []core.CheckpointResult {
	results := make([]core.CheckpointResult, len(runs))
	done := make([]bool, len(runs))
	// closed is set once the phase is over, so a run that finishes after the
	// budget neither overwrites nor reports again the timeout it was given.
	closed := false
	var mu sync.Mutex
	var wg sync.WaitGroup
	phase, cancel := context.WithTimeout(context.Background(), overall)
	defer cancel()
	for i, run := range runs {
		wg.Add(1)
		go func(i int, run checkpointRun) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(phase, perRun)
			defer cancel()
			result := checkpointCheckout(ctx, git, run)
			mu.Lock()
			if closed {
				mu.Unlock()
				return
			}
			results[i], done[i] = result, true
			mu.Unlock()
			if report != nil {
				report(i, result)
			}
		}(i, run)
	}
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()
	select {
	case <-finished:
	case <-phase.Done():
	}
	mu.Lock()
	closed = true
	var late []int
	for i, run := range runs {
		if !done[i] {
			results[i] = core.CheckpointResult{Target: run.Target, Number: run.Number, Outcome: checkpointTimedOut, Detail: fmt.Sprintf("still running after the %s shutdown budget", overall)}
			late = append(late, i)
		}
	}
	mu.Unlock()
	if report != nil {
		for _, i := range late {
			report(i, results[i])
		}
	}
	return results
}

// checkpointGit runs one git command in dir and returns its trimmed output.
type checkpointGit func(ctx context.Context, dir string, args ...string) (string, error)

func runCheckpointGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	// A checkpoint runs unattended on the way out, so git must fail rather
	// than wait on a credential prompt nobody will answer.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	// A deadline ends git and whatever it started, such as a credential
	// helper or ssh, and stops waiting on their pipes soon after, so the run
	// budget holds even for a push stuck on the network.
	cmd.Cancel = func() error { return process.Kill(cmd) }
	cmd.WaitDelay = checkpointGitWaitDelay
	output, err := process.CombinedOutput(cmd)
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, errors.New(lastOutputLine(text))
	}
	return text, nil
}

func lastOutputLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// checkpointCheckout commits a run's uncommitted changes as a shutdown
// checkpoint and pushes its branch, with a normal push, to the branch it
// tracks. A clean, fully pushed checkout is left alone, so a second shutdown
// with nothing new adds no commit. It refuses anything other than the run's
// own working branch: a detached HEAD, a branch with no upstream, a branch
// tracking a differently named one, or the remote's default branch.
func checkpointCheckout(ctx context.Context, git checkpointGit, run checkpointRun) (result core.CheckpointResult) {
	result = core.CheckpointResult{Target: run.Target, Number: run.Number}
	defer func() {
		if result.Outcome == checkpointFailed && ctx.Err() != nil {
			result.Outcome = checkpointTimedOut
		}
	}()
	fail := func(format string, args ...interface{}) core.CheckpointResult {
		result.Outcome, result.Detail = checkpointFailed, fmt.Sprintf(format, args...)
		return result
	}
	if run.Checkout == "" {
		result.Outcome, result.Detail = checkpointNothing, "no checkout yet"
		return result
	}
	if info, err := os.Stat(run.Checkout); err != nil || !info.IsDir() {
		result.Outcome, result.Detail = checkpointNothing, "checkout "+run.Checkout+" no longer exists"
		return result
	}
	if _, err := git(ctx, run.Checkout, "rev-parse", "--is-inside-work-tree"); err != nil {
		return fail("%s is not a git checkout: %v", run.Checkout, err)
	}
	branch, err := git(ctx, run.Checkout, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch == "" {
		return fail("HEAD is detached, so there is no branch to push")
	}
	result.Branch = branch
	upstream, err := git(ctx, run.Checkout, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil || upstream == "" {
		return fail("branch %s has no upstream to push to", branch)
	}
	remote, err := git(ctx, run.Checkout, "config", "branch."+branch+".remote")
	if err != nil || remote == "" {
		return fail("branch %s has no remote configured", branch)
	}
	if upstream != remote+"/"+branch {
		return fail("branch %s tracks %s rather than its own branch on %s", branch, upstream, remote)
	}
	if isBaseBranch(ctx, git, run.Checkout, remote, branch) {
		return fail("branch %s is the base branch of %s; only a run's own working branch is pushed", branch, remote)
	}
	for _, marker := range inProgressMarkers {
		gitPath, err := git(ctx, run.Checkout, "rev-parse", "--git-path", marker.path)
		if err != nil {
			continue
		}
		if !filepath.IsAbs(gitPath) {
			gitPath = filepath.Join(run.Checkout, gitPath)
		}
		if _, err := os.Stat(gitPath); err == nil {
			return fail("a %s is in progress, so the tree cannot be committed", marker.name)
		}
	}
	// Ask the remote before touching the tree, so an unreachable remote or a
	// branch that moved on without this checkout fails with the work left
	// exactly as the agent left it.
	remoteHead, err := git(ctx, run.Checkout, "ls-remote", "--heads", remote, "refs/heads/"+branch)
	if err != nil {
		return fail("remote %s is unreachable: %v", remote, err)
	}
	if fields := strings.Fields(remoteHead); len(fields) > 0 {
		if _, err := git(ctx, run.Checkout, "merge-base", "--is-ancestor", fields[0], "HEAD"); err != nil {
			return fail("%s on %s has commits this checkout does not, so pushing would not fast-forward", branch, remote)
		}
	}
	status, err := git(ctx, run.Checkout, "status", "--porcelain")
	if err != nil {
		return fail("git status: %v", err)
	}
	committed := false
	if status != "" {
		if _, err := git(ctx, run.Checkout, "add", "--all"); err != nil {
			return fail("git add: %v", err)
		}
		if _, err := git(ctx, run.Checkout, "commit", "--quiet", "-m", checkpointMessage(run.Number)); err != nil {
			return fail("git commit: %v", err)
		}
		committed = true
	}
	if !committed && len(strings.Fields(remoteHead)) > 0 {
		ahead, err := git(ctx, run.Checkout, "rev-list", "--count", strings.Fields(remoteHead)[0]+"..HEAD")
		if err != nil {
			return fail("count unpushed commits: %v", err)
		}
		if count, _ := strconv.Atoi(ahead); count == 0 {
			result.Outcome = checkpointNothing
			return result
		}
	}
	// A plain push of the branch to its own name: never forced, so a branch
	// that moved on the remote is a failure to report, not one to overwrite.
	if _, err := git(ctx, run.Checkout, "push", "--quiet", remote, "refs/heads/"+branch+":refs/heads/"+branch); err != nil {
		if committed {
			// Take the commit back so the checkout is left as the agent left
			// it, with its changes still uncommitted in the working tree.
			_, _ = git(context.Background(), run.Checkout, "reset", "--quiet", "--mixed", "HEAD^")
		}
		return fail("push to %s rejected or unreachable: %v", remote, err)
	}
	result.Outcome = checkpointPushed
	if committed {
		result.Detail = "uncommitted changes committed as a shutdown checkpoint"
	}
	return result
}

// isBaseBranch reports whether branch is the remote's default branch, or
// main or master when the remote's default is unknown.
func isBaseBranch(ctx context.Context, git checkpointGit, dir, remote, branch string) bool {
	if head, err := git(ctx, dir, "symbolic-ref", "--quiet", "refs/remotes/"+remote+"/HEAD"); err == nil && head != "" {
		return strings.TrimPrefix(head, "refs/remotes/"+remote+"/") == branch
	}
	return branch == "main" || branch == "master"
}

// checkpointMessage names a shutdown checkpoint commit in the style of the
// progress checkpoints gh-fix pushes during a run, so CI skips it as it
// skips those.
func checkpointMessage(number int) string {
	return fmt.Sprintf("Shutdown checkpoint for issue #%d [skip ci]", number)
}

// checkouts reports every in-flight run with the checkout it announced, which
// is empty while the agent has not announced one yet.
func (a *activeAgents) checkouts() []checkpointRun {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	runs := make([]checkpointRun, 0, len(a.runs))
	for run := range a.runs {
		if run.issue.ScheduleID != "" {
			continue
		}
		runs = append(runs, checkpointRun{Target: run.issue.Target, Number: run.issue.Number, Checkout: run.checkout})
	}
	return runs
}
