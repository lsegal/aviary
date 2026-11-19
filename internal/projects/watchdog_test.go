package projects

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
)

// unblockCalls records every unblock agent call a test's watchdog makes, and
// answers each with the next scripted reply.
type unblockCalls struct {
	mu      sync.Mutex
	specs   []agentSpec
	prompts []string
	replies []string
	made    chan struct{}
}

func newUnblockCalls(replies ...string) *unblockCalls {
	return &unblockCalls{replies: replies, made: make(chan struct{}, 16)}
}

func (c *unblockCalls) run(_ context.Context, spec agentSpec, prompt string) (string, error) {
	c.mu.Lock()
	c.specs = append(c.specs, spec)
	c.prompts = append(c.prompts, prompt)
	reply := ""
	if len(c.replies) > 0 {
		reply, c.replies = c.replies[0], c.replies[1:]
	}
	c.mu.Unlock()
	c.made <- struct{}{}
	return reply, nil
}

func (c *unblockCalls) await(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-c.made:
		case <-time.After(5 * time.Second):
			t.Fatalf("unblock call %d of %d never ran", i+1, n)
		}
	}
}

func (c *unblockCalls) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.prompts)
}

func (c *unblockCalls) prompt(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prompts[i]
}

// jobList is a job list a test can change while the watchdog reads it.
type jobList struct {
	mu   sync.Mutex
	jobs []JobSnapshot
}

func (l *jobList) set(jobs ...JobSnapshot) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.jobs = append([]JobSnapshot(nil), jobs...)
}

func (l *jobList) get() []JobSnapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]JobSnapshot(nil), l.jobs...)
}

// awaitLog waits for the watchdog to log want, which it does after the call
// a test synchronizes on has returned.
func awaitLog(t *testing.T, logs *syncBuffer, want string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if strings.Contains(logs.String(), want) {
			return
		}
	}
	t.Fatalf("log never showed %q:\n%s", want, logs.String())
}

func watchdogGlorp(calls *unblockCalls, logs *syncBuffer) *Glorp {
	w := &Glorp{Identity: "ABCD1234", SelfLogin: "octo", Out: logs, Registry: agents.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, unblockRun: calls.run}
	w.SetUnblockInterval(30 * time.Millisecond)
	return w
}

func TestDetectStuckJobsPicksPausedAndSilentJobsOnly(t *testing.T) {
	jobs := []JobSnapshot{
		{Target: "o/r", Number: 1, Status: "paused", WaitReason: "its stacked pull request waits on its blocker to merge"},
		{Target: "o/r", Number: 2, Status: "paused", WaitReason: core.ManualPauseReason},
		{Target: "o/r", Number: 3, Status: "active", Log: "same"},
		{Target: "o/r", Number: 4, Status: "active", Log: "moved on"},
		{Target: "o/r", Number: 5, Status: "active", Log: "never seen"},
		{Target: "o/r", Number: 6, Status: "queued"},
		{Target: "o/r", Number: 7, Status: "waiting"},
	}
	baseline := map[string]string{"o/r#3": "same", "o/r#4": "before", "o/r#6": "", "o/r#7": ""}
	history := map[string][]string{"o/r#1": {"commented"}, "o/r#4": {"old"}}
	stuck := detectStuckJobs(jobs, baseline, history, 5*time.Minute)
	var keys []string
	for _, item := range stuck {
		keys = append(keys, item.Key)
	}
	if got := strings.Join(keys, ","); got != "o/r#1,o/r#3" {
		t.Fatalf("stuck jobs = %s, want o/r#1,o/r#3", got)
	}
	if !strings.Contains(stuck[0].Why, "paused: its stacked pull request") {
		t.Errorf("paused reason = %q", stuck[0].Why)
	}
	if !strings.Contains(stuck[1].Why, "no output for at least 5m") {
		t.Errorf("silent reason = %q", stuck[1].Why)
	}
	if len(stuck[0].Tried) != 1 || stuck[0].Tried[0] != "commented" {
		t.Errorf("paused job's earlier interventions = %v", stuck[0].Tried)
	}
	if _, ok := history["o/r#4"]; ok {
		t.Error("a job that is no longer stuck kept its history")
	}
}

func TestParseUnblockActionsReadsTheClosingReport(t *testing.T) {
	output := "I checked everything.\n- `UNBLOCK o/r#1 comment: https://github.com/o/r/pull/9#issuecomment-1 told the author the blocker merged`\nUNBLOCK o/r#2 local: retried it through the dashboard\nUNBLOCK o/r#3 none\nnot a report UNBLOCK o/r#4 none: x\n"
	actions := parseUnblockActions(output)
	if got := actions["o/r#1"]; got.Kind != "comment" || got.Detail != "https://github.com/o/r/pull/9#issuecomment-1 told the author the blocker merged" {
		t.Errorf("o/r#1 = %+v", got)
	}
	if got := actions["o/r#2"]; got.Kind != "local" || got.Detail != "retried it through the dashboard" {
		t.Errorf("o/r#2 = %+v", got)
	}
	if got := actions["o/r#3"]; got.Kind != "none" || got.Detail != "(no detail given)" {
		t.Errorf("o/r#3 = %+v", got)
	}
	if _, ok := actions["o/r#4"]; ok {
		t.Error("a report buried mid-line was read")
	}
}

// TestWatchdogRunsOneCallForEveryStuckJobAndEscalates covers the round's
// single call for interrelated jobs, the log record, and a job still stuck in
// the next round being included again with what was tried.
func TestWatchdogRunsOneCallForEveryStuckJobAndEscalates(t *testing.T) {
	calls := newUnblockCalls(
		"UNBLOCK o/r#1 comment: https://github.com/o/r/pull/11#issuecomment-5 blocker merged, rebase\nUNBLOCK o/r#2 local: retried through the dashboard",
		"UNBLOCK o/r#1 local: rebased the stack",
	)
	logs := &syncBuffer{}
	w := watchdogGlorp(calls, logs)
	list := &jobList{}
	list.set(
		JobSnapshot{Target: "o/r", Number: 1, Status: "paused", WaitReason: "its stacked pull request waits on its blocker to merge", PullRequest: 11},
		JobSnapshot{Target: "o/r", Number: 2, Status: "paused", WaitReason: "its stacked pull request waits on its blocker to merge", PullRequest: 12},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		w.watchStuckJobs(ctx, list.get)
		close(done)
	}()
	calls.await(t, 1)
	first := calls.prompt(0)
	for _, want := range []string{"key: o/r#1", "key: o/r#2", "https://github.com/o/r/pull/11", "@/glorp:ABCD1234", "@octo", "UNBLOCK KEY comment"} {
		if !strings.Contains(first, want) {
			t.Errorf("first prompt is missing %q:\n%s", want, first)
		}
	}
	// Job 2 finished; job 1 is still stuck.
	list.set(
		JobSnapshot{Target: "o/r", Number: 1, Status: "paused", WaitReason: "its stacked pull request waits on its blocker to merge", PullRequest: 11},
		JobSnapshot{Target: "o/r", Number: 2, Status: "completed"},
	)
	calls.await(t, 1)
	second := calls.prompt(1)
	if !strings.Contains(second, "key: o/r#1") || strings.Contains(second, "key: o/r#2") {
		t.Errorf("second round covered the wrong jobs:\n%s", second)
	}
	if !strings.Contains(second, "already tried in earlier rounds") || !strings.Contains(second, "comment: https://github.com/o/r/pull/11#issuecomment-5 blocker merged, rebase") {
		t.Errorf("second round does not carry the comment already posted:\n%s", second)
	}
	awaitLog(t, logs, "o/r#1 got a local task: rebased the stack")
	cancel()
	<-done
	got := logs.String()
	for _, want := range []string{
		"stuck-job watchdog found 2 stuck job(s): o/r#1 (paused: its stacked pull request waits on its blocker to merge); o/r#2",
		"running one unblock agent call (claude/opus) for 2 job(s)",
		"o/r#1 got a comment: https://github.com/o/r/pull/11#issuecomment-5",
		"o/r#2 got a local task: retried through the dashboard",
		"o/r#1 got a local task: rebased the stack",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("log is missing %q:\n%s", want, got)
		}
	}
}

func TestWatchdogFindsAnActiveJobWhoseOutputStopped(t *testing.T) {
	calls := newUnblockCalls()
	logs := &syncBuffer{}
	w := watchdogGlorp(calls, logs)
	list := &jobList{}
	list.set(JobSnapshot{Target: "o/r", Number: 5, Status: "active", Log: "waiting forever\n"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.watchStuckJobs(ctx, list.get)
	calls.await(t, 1)
	prompt := calls.prompt(0)
	if !strings.Contains(prompt, "key: o/r#5") || !strings.Contains(prompt, "written no output") || !strings.Contains(prompt, "waiting forever") {
		t.Errorf("prompt does not describe the silent job:\n%s", prompt)
	}
	awaitLog(t, logs, "the unblock agent reported no action for o/r#5")
}

func TestWatchdogLeavesHealthyJobsAlone(t *testing.T) {
	calls := newUnblockCalls()
	w := watchdogGlorp(calls, &syncBuffer{})
	var mu sync.Mutex
	step := 0
	jobs := func() []JobSnapshot {
		mu.Lock()
		defer mu.Unlock()
		step++
		return []JobSnapshot{
			{Target: "o/r", Number: 1, Status: "active", Log: strings.Repeat("x", step)},
			{Target: "o/r", Number: 2, Status: "paused", WaitReason: core.ManualPauseReason},
			{Target: "o/r", Number: 3, Status: "queued"},
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	w.watchStuckJobs(ctx, jobs)
	if calls.count() != 0 {
		t.Fatalf("healthy jobs got %d unblock call(s):\n%s", calls.count(), calls.prompt(0))
	}
}

func TestWatchdogIntervalChangesTakeEffect(t *testing.T) {
	calls := newUnblockCalls()
	w := watchdogGlorp(calls, &syncBuffer{})
	w.SetUnblockInterval(0)
	list := &jobList{}
	list.set(JobSnapshot{Target: "o/r", Number: 1, Status: "paused", WaitReason: "x"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.watchStuckJobs(ctx, list.get)
	time.Sleep(100 * time.Millisecond)
	if calls.count() != 0 {
		t.Fatal("a watchdog turned off made an unblock call")
	}
	w.SetUnblockInterval(20 * time.Millisecond)
	calls.await(t, 1)
}

func TestUnblockSpecDefaultsToTheCodingAgentAndTakesOverrides(t *testing.T) {
	w := &Glorp{Registry: agents.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}}
	spec, err := w.unblockSpec()
	if err != nil || spec.Name != "claude" || spec.Model != "opus" {
		t.Fatalf("default unblock spec = %+v, %v; want claude/opus", spec, err)
	}
	override := []string{"codex"}
	w.agentOverride.Store(&override)
	if spec, _ := w.unblockSpec(); spec.Name != "codex" {
		t.Fatalf("unblock spec after a live agent change = %+v, want codex", spec)
	}
	w.SetUnblockAgent(unblockConfig{Agent: "claude", Model: "sonnet"})
	if spec, err := w.unblockSpec(); err != nil || spec.Name != "claude" || spec.Model != "sonnet" {
		t.Fatalf("overridden unblock spec = %+v, %v; want claude/sonnet", spec, err)
	}
	w.SetUnblockAgent(unblockConfig{Model: "gpt-5.5"})
	if spec, err := w.unblockSpec(); err != nil || spec.Name != "codex" || spec.Model != "gpt-5.5" {
		t.Fatalf("model-only override = %+v, %v; want codex/gpt-5.5", spec, err)
	}
}

func TestParseUnblockInterval(t *testing.T) {
	for value, want := range map[string]time.Duration{"": 0, "0": 0, "0s": 0, "5m": 5 * time.Minute, " 1h30m ": 90 * time.Minute} {
		got, err := parseUnblockInterval(value)
		if err != nil || got != want {
			t.Errorf("parseUnblockInterval(%q) = %v, %v; want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"10s", "-5m", "soon"} {
		if _, err := parseUnblockInterval(value); err == nil {
			t.Errorf("parseUnblockInterval(%q) accepted a bad interval", value)
		}
	}
}

func TestGlorpApplySettingsChangesTheWatchdog(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{}}}
	w := &Glorp{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Registry: agents.MustBuiltin(), Out: &syncBuffer{}}
	w.SetUnblockInterval(defaultUnblockInterval)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	var snapshot SettingsSnapshot
	var err error
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if snapshot, err = w.CurrentSettings(ctx); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.UnblockInterval != "15m" || snapshot.UnblockAgent != "" || snapshot.UnblockDefault != "claude/opus" {
		t.Fatalf("default watchdog settings = %+v", snapshot)
	}
	interval, agent, model := "10m", "codex", "gpt-5.5"
	snapshot, err = w.ApplySettings(ctx, SettingsUpdate{UnblockInterval: &interval, UnblockAgent: &agent, UnblockModel: &model})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.UnblockInterval != "10m" || snapshot.UnblockAgent != "codex" || snapshot.UnblockModel != "gpt-5.5" || w.UnblockInterval() != 10*time.Minute {
		t.Fatalf("watchdog settings after update = %+v", snapshot)
	}
	if spec, _ := w.unblockSpec(); spec.String() != "codex/gpt-5.5" {
		t.Fatalf("unblock spec after update = %s", spec)
	}
	bad := "10s"
	if _, err := w.ApplySettings(ctx, SettingsUpdate{UnblockInterval: &bad}); err == nil {
		t.Error("a 10s watchdog interval was accepted")
	}
	unknown := "nope"
	if _, err := w.ApplySettings(ctx, SettingsUpdate{UnblockAgent: &unknown}); err == nil {
		t.Error("an unknown unblock agent was accepted")
	}
}

// TestRunWatchdogCoversASilentRunningJob runs the watch loop with a job whose
// agent never writes, and checks the watchdog's one call covers it.
func TestRunWatchdogCoversASilentRunningJob(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{{Number: 7, Title: "Silent"}}}}
	r := &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 1)}
	calls := newUnblockCalls()
	w := &Glorp{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: r, Out: &syncBuffer{}, unblockRun: calls.run}
	w.SetUnblockAgent(unblockConfig{Agent: "claude"})
	w.SetUnblockInterval(40 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	<-r.dispatched
	calls.await(t, 1)
	if prompt := calls.prompt(0); !strings.Contains(prompt, "key: o/r#7") || !strings.Contains(prompt, "title: Silent") {
		t.Errorf("unblock call does not cover the silent job:\n%s", prompt)
	}
	close(r.release)
	cancel()
	<-done
}

// TestWatchdogLogsAFailedUnblockCallsOutput covers a call that exits non-zero:
// the agent's own output, not just the exit code, reaches the log and each
// job's history, a report printed before the exit reads as that report, and a
// silent failure says the agent produced no output (issue #793).
func TestWatchdogLogsAFailedUnblockCallsOutput(t *testing.T) {
	stuck := []stuckJob{
		{Key: "o/r#1", Job: JobSnapshot{Target: "o/r", Number: 1, Status: "paused"}, Why: "paused"},
		{Key: "o/r#2", Job: JobSnapshot{Target: "o/r", Number: 2, Status: "paused"}, Why: "paused"},
	}
	exit := errors.New("exit status 1")
	for _, tc := range []struct {
		name    string
		output  string
		logs    []string
		history []string
		absent  []string
	}{
		{
			name:    "explanation",
			output:  "checking o/r#1\nAgent error: gh is not signed in, so no job could be checked\n",
			logs:    []string{"unblock agent failed: exit status 1; its last output:\n  checking o/r#1\n  Agent error: gh is not signed in"},
			history: []string{"the unblock agent failed (exit status 1) before acting, saying: checking o/r#1 / Agent error: gh is not signed in, so no job could be checked"},
			absent:  []string{"produced no output"},
		},
		{
			name:    "silent",
			output:  "  \n",
			logs:    []string{"unblock agent failed: exit status 1, and the agent produced no output"},
			history: []string{"the unblock agent failed (exit status 1) with no output before acting"},
		},
		{
			name:    "reported",
			output:  "UNBLOCK o/r#1 none: its blocker is still open, nothing can unblock it yet\n",
			logs:    []string{"unblock agent exited with exit status 1 after reporting its actions", "o/r#1 needed no action: its blocker is still open", "reported no action for o/r#2"},
			history: []string{"none: its blocker is still open, nothing can unblock it yet", "the unblock agent exited with exit status 1 and reported no action"},
			absent:  []string{"unblock agent failed"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &syncBuffer{}
			w := watchdogGlorp(newUnblockCalls(), logs)
			w.unblockRun = func(context.Context, agentSpec, string) (string, error) { return tc.output, exit }
			history := map[string][]string{}
			w.runUnblockRound(context.Background(), stuck, history, time.Minute)
			got := logs.String()
			for _, want := range tc.logs {
				if !strings.Contains(got, want) {
					t.Errorf("log is missing %q:\n%s", want, got)
				}
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(got, unwanted) {
					t.Errorf("log has %q:\n%s", unwanted, got)
				}
			}
			recorded := strings.Join(append(history["o/r#1"], history["o/r#2"]...), "\n")
			for _, want := range tc.history {
				if !strings.Contains(recorded, want) {
					t.Errorf("history is missing %q:\n%s", want, recorded)
				}
			}
			if tc.name != "reported" && (len(history["o/r#1"]) != 1 || len(history["o/r#2"]) != 1) {
				t.Errorf("each job should get one history entry: %v", history)
			}
		})
	}
}
