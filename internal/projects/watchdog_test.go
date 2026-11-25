package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
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

func watchdogEngine(calls *unblockCalls, logs *syncBuffer) *Engine {
	w := &Engine{Identity: "ABCD1234", SelfLogin: "octo", Out: logs, Registry: clidefs.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, unblockRun: calls.run}
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
	w := watchdogEngine(calls, logs)
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
	for _, want := range []string{"key: o/r#1", "key: o/r#2", "https://github.com/o/r/pull/11", "@/aviary:ABCD1234", "@octo", "UNBLOCK KEY comment"} {
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
	w := watchdogEngine(calls, logs)
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
	w := watchdogEngine(calls, &syncBuffer{})
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
	w := watchdogEngine(calls, &syncBuffer{})
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
	w := &Engine{Registry: clidefs.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}}
	spec, err := w.unblockSpec()
	if err != nil || spec.Name != "claude" || spec.Model != "opus" {
		t.Fatalf("default unblock spec = %+v, %v; want claude/opus", spec, err)
	}
	w.SetUnblockAgent(unblockConfig{Agent: "claude", Model: "sonnet"})
	if spec, err := w.unblockSpec(); err != nil || spec.Name != "claude" || spec.Model != "sonnet" {
		t.Fatalf("overridden unblock spec = %+v, %v; want claude/sonnet", spec, err)
	}
	w.SetUnblockAgent(unblockConfig{Model: "haiku"})
	if spec, err := w.unblockSpec(); err != nil || spec.Name != "claude" || spec.Model != "haiku" {
		t.Fatalf("model-only override = %+v, %v; want claude/haiku", spec, err)
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

func TestEngineApplySettingsChangesTheWatchdog(t *testing.T) {
	dir := t.TempDir()
	src := &fakeSource{batches: [][]Issue{{}}}
	w := &Engine{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Registry: clidefs.MustBuiltin(), Out: &syncBuffer{}}
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
	w := &Engine{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: src, Runner: r, Out: &syncBuffer{}, unblockRun: calls.run}
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
// silent failure says the agent produced no output.
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
			w := watchdogEngine(newUnblockCalls(), logs)
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

// TestTriggerUnblockRunsRequestedRounds covers rounds asked for from the prompt
// area while the timed watchdog is off: a named job is covered even though it
// is not stuck, and a request that names a discussion, an unknown job, or
// finds nothing stuck is logged without an agent call.
func TestTriggerUnblockRunsRequestedRounds(t *testing.T) {
	calls := newUnblockCalls("UNBLOCK o/r#3 none: its CI is still running")
	logs := &syncBuffer{}
	w := watchdogEngine(calls, logs)
	w.SetUnblockInterval(0)
	list := &jobList{}
	list.set(
		JobSnapshot{Target: "o/r", Number: 3, Status: "active", WaitReason: "waiting on CI", Log: "running tests"},
		JobSnapshot{Target: "https://github.com/o/r/discussions", Number: 4, Status: "active"},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.watchStuckJobs(ctx, list.get)

	if err := w.TriggerUnblock(" o/r#3 "); err != nil {
		t.Fatal(err)
	}
	calls.await(t, 1)
	prompt := calls.prompt(0)
	if !strings.Contains(prompt, "key: o/r#3") || !strings.Contains(prompt, "stuck because: named from the prompt area while active: waiting on CI") {
		t.Fatalf("requested round prompt does not cover the named job:\n%s", prompt)
	}
	awaitLog(t, logs, "o/r#3 needed no action: its CI is still running")

	for _, tc := range []struct{ key, want string }{
		{"https://github.com/o/r/discussions#4", "the unblock round requested for https://github.com/o/r/discussions#4 cannot run: it is a discussion"},
		{"o/r#99", "the unblock round requested for o/r#99 cannot run: aviary has no such job"},
		{"", "the unblock round requested from the prompt area found no stuck jobs"},
	} {
		if err := w.TriggerUnblock(tc.key); err != nil {
			t.Fatal(err)
		}
		awaitLog(t, logs, tc.want)
	}
	if calls.count() != 1 {
		t.Fatalf("unblock calls = %d, want only the named job's", calls.count())
	}
}

// TestTriggerUnblockRefusesASecondPendingRequest checks a request made while
// another is still waiting for the watchdog fails instead of blocking.
func TestTriggerUnblockRefusesASecondPendingRequest(t *testing.T) {
	w := &Engine{}
	if err := w.TriggerUnblock("o/r#1"); err != nil {
		t.Fatal(err)
	}
	if err := w.TriggerUnblock("o/r#2"); err == nil || !strings.Contains(err.Error(), "already requested") {
		t.Fatalf("second request error = %v, want it refused", err)
	}
	if key := <-w.unblockTriggers(); key != "o/r#1" {
		t.Fatalf("pending request = %q, want the first one", key)
	}
}

// TestRequestedStuckJobsWithoutAKeyNeedsAFullWatch checks an unnamed request
// made sooner than the shortest interval after the last look covers only
// paused jobs, since a running job has not been silent long enough, while a
// key naming a stuck job returns that job alone with its history.
func TestRequestedStuckJobsWithoutAKeyNeedsAFullWatch(t *testing.T) {
	jobs := []JobSnapshot{
		{Target: "o/r", Number: 1, Status: "active", Log: "same"},
		{Target: "o/r", Number: 2, Status: "paused", WaitReason: "merge conflict"},
	}
	baseline := scrollbacks(jobs)
	history := map[string][]string{"o/r#2": {"posted a comment"}}
	stuck, err := requestedStuckJobs(jobs, baseline, history, 10*time.Second, "")
	if err != nil || len(stuck) != 1 || stuck[0].Key != "o/r#2" {
		t.Fatalf("early unnamed request = %+v, %v; want the paused job only", stuck, err)
	}
	stuck, err = requestedStuckJobs(jobs, baseline, history, 2*time.Minute, "o/r#2")
	if err != nil || len(stuck) != 1 || stuck[0].Why != "paused: merge conflict" || !slices.Equal(stuck[0].Tried, []string{"posted a comment"}) {
		t.Fatalf("named stuck job = %+v, %v", stuck, err)
	}
	stuck, err = requestedStuckJobs(jobs, baseline, history, 2*time.Minute, "")
	if err != nil || len(stuck) != 2 {
		t.Fatalf("unnamed request after a full interval = %+v, %v; want both jobs", stuck, err)
	}
}

// TestValidateUnblockAgentChecksTheOverride checks the unblock override is
// held to the registry: the agent must exist, and a model must be one it
// accepts, while an empty agent defers to the coding agent.
func TestValidateUnblockAgentChecksTheOverride(t *testing.T) {
	registry := acmeRegistry(t)
	for _, tc := range []struct {
		config unblockConfig
		ok     bool
	}{
		{unblockConfig{}, true},
		{unblockConfig{Model: "anything"}, true},
		{unblockConfig{Agent: "acme"}, true},
		{unblockConfig{Agent: "acme", Model: "acme-1"}, true},
		{unblockConfig{Agent: "nosuchagent"}, false},
		{unblockConfig{Agent: "acme", Model: "acme-2"}, false},
	} {
		if err := validateUnblockAgent(registry, tc.config); (err == nil) != tc.ok {
			t.Errorf("validateUnblockAgent(%+v) = %v, want ok=%v", tc.config, err, tc.ok)
		}
	}
}

// acmeRegistry registers one agent that enumerates the models it accepts.
func acmeRegistry(t *testing.T) *clidefs.Registry {
	t.Helper()
	registry, err := clidefs.NewRegistry(clidefs.Definition{
		Name: "acme", Binary: "acme", Models: clidefs.NewAllowList("acme-1"),
		Args:    clidefs.Args{Run: []clidefs.Fragment{{Args: []string{"{prompt}"}}}, Resume: []clidefs.Fragment{{Args: []string{"{prompt}"}}}},
		Session: clidefs.Session{Assign: clidefs.AssignNone},
		Output:  clidefs.Output{Format: clidefs.FormatText},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// TestOneShotSpecRejectsWhatItCannotRun checks each reason an out-of-band
// call cannot resolve its agent is reported rather than run.
func TestOneShotSpecRejectsWhatItCannotRun(t *testing.T) {
	registry := acmeRegistry(t)
	for _, tc := range []struct {
		name   string
		engine *Engine
		config unblockConfig
		want   string
	}{
		{"no coding agent", &Engine{Registry: registry, Runner: &fakeRunner{}}, unblockConfig{}, "no coding agent is configured"},
		{"unknown override", &Engine{Registry: registry}, unblockConfig{Agent: "nosuchagent"}, "nosuchagent"},
		{"model the coding agent refuses", &Engine{Registry: registry, Runner: CommandRunner{Agent: "acme", Definitions: registry}}, unblockConfig{Model: "acme-2"}, "acme-1"},
		{"coding agent outside the registry", &Engine{Registry: registry, Runner: CommandRunner{Agent: "ghost"}}, unblockConfig{Model: "m"}, "ghost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if spec, err := tc.engine.oneShotSpec(tc.config); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("oneShotSpec = %+v, %v; want an error naming %q", spec, err, tc.want)
			}
		})
	}
}

// TestUnblockRoundReportsWhyItCannotRun checks a round whose agent cannot be
// resolved, or which has no command runner to run it, logs why and records
// nothing.
func TestUnblockRoundReportsWhyItCannotRun(t *testing.T) {
	stuck := []stuckJob{{Key: "o/r#1", Job: JobSnapshot{Target: "o/r", Number: 1, Status: "paused"}, Why: "paused"}}
	for _, tc := range []struct {
		name     string
		runner   AgentRunner
		override string
		want     string
	}{
		{"unknown agent", CommandRunner{Agent: "claude"}, "nosuchagent", "cannot run the unblock agent: unknown agent"},
		{"no command runner", &fakeRunner{}, "claude", "cannot run the unblock agent: no coding agent is configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := &syncBuffer{}
			w := &Engine{Registry: clidefs.MustBuiltin(), Runner: tc.runner, Out: logs}
			w.SetUnblockAgent(unblockConfig{Agent: tc.override})
			history := map[string][]string{}
			w.runUnblockRound(context.Background(), stuck, history, time.Minute)
			if got := logs.String(); !strings.Contains(got, tc.want) {
				t.Fatalf("log = %q, want %q", got, tc.want)
			}
			if len(history) != 0 {
				t.Fatalf("history = %v, want nothing recorded for a round that never ran", history)
			}
		})
	}
}

// TestUnblockRoundRunsTheCodingAgentOutOfBand runs a real round through the
// command runner: the stand-in agent is launched with the round's prompt, and
// a reply without a closing report is recorded as such.
func TestUnblockRoundRunsTheCodingAgentOutOfBand(t *testing.T) {
	registry := oneShotRegistry(t)
	record := filepath.Join(t.TempDir(), "prompt.md")
	t.Setenv(oneShotHelperRecordEnv, record)
	logs := &syncBuffer{}
	w := &Engine{Identity: "ABCD1234", Registry: registry, Runner: CommandRunner{Agent: "argvagent", Definitions: registry}, Out: logs}
	stuck := []stuckJob{{Key: "o/r#1", Job: JobSnapshot{Target: "o/r", Number: 1, Status: "paused", Title: "Flaky build"}, Why: "paused"}}
	history := map[string][]string{}
	w.runUnblockRound(context.Background(), stuck, history, time.Minute)
	got, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "key: o/r#1") || !strings.Contains(string(got), "title: Flaky build") {
		t.Fatalf("agent received prompt:\n%s", got)
	}
	if !strings.Contains(logs.String(), "running one unblock agent call (argvagent) for 1 job(s)") || !strings.Contains(logs.String(), "reported no action for o/r#1") {
		t.Fatalf("log = %q", logs.String())
	}
	if len(history["o/r#1"]) != 1 || !strings.Contains(history["o/r#1"][0], "the unblock agent ran but reported no action") {
		t.Fatalf("history = %v", history)
	}
}

// TestRunUnblockReturnsTheAgentsText checks the one-shot call returns what
// the agent wrote, and refuses an agent the registry does not define.
func TestRunUnblockReturnsTheAgentsText(t *testing.T) {
	registry := oneShotRegistry(t)
	runner := CommandRunner{Agent: "stdinagent", Definitions: registry}
	output, err := runner.runUnblock(context.Background(), agentSpec{Name: "stdinagent"}, "unblock o/r#1")
	if err != nil {
		t.Fatalf("runUnblock: %v (output %q)", err, output)
	}
	if got := strings.TrimSpace(output); got != "arguments=0 stdin=13 file=0" {
		t.Fatalf("agent output = %q", got)
	}
	if _, err := runner.runUnblock(context.Background(), agentSpec{Name: "ghost"}, "x"); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("unknown agent error = %v", err)
	}
}
