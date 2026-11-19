package projects

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
)

// promptReporter records the conversations a run reports to its prompt area.
type promptReporter struct {
	snapshotReporter
	updates chan []core.PromptEntry
}

func (r *promptReporter) Prompts(entries []core.PromptEntry) { r.updates <- entries }

// awaitPrompt waits for the entry with id to be answered and returns it.
func awaitPrompt(t *testing.T, w *Glorp, id int) core.PromptEntry {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, entry := range w.Prompts() {
			if entry.ID == id && entry.Status != "pending" {
				return entry
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("prompt %d was never answered: %+v", id, w.Prompts())
	return core.PromptEntry{}
}

func TestParsePromptActionsReadsActionLines(t *testing.T) {
	output := strings.Join([]string{
		"Job o/r#7 waits on #12, which is still open.",
		"GLORP pause o/r#7",
		"- `GLORP resume o/r#8`",
		"  glorp RETRY o/r#9",
		"GLORP unblock all",
		"GLORP pause o/r#7",
		"Run GLORP stop o/r#10 if you want it gone.",
		"GLORP delete o/r#11",
	}, "\n")
	got := parsePromptActions(output)
	want := []promptAction{{"pause", "o/r#7"}, {"resume", "o/r#8"}, {"retry", "o/r#9"}, {"unblock", "all"}}
	if len(got) != len(want) {
		t.Fatalf("actions = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("action %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	response := promptResponse(output)
	if strings.Contains(response, "GLORP pause") || strings.Contains(response, "GLORP unblock") || !strings.Contains(response, "waits on #12") {
		t.Errorf("response keeps action lines or loses the answer:\n%s", response)
	}
}

func TestPromptAgentPromptIsScopedToTheInstance(t *testing.T) {
	prompt := promptAgentPrompt(promptState{
		Identity:  "ABCD1234",
		SelfLogin: "octo",
		Version:   "v9.9.9",
		Snapshot: core.Snapshot{
			Targets: []string{"o/r"}, IssueCounts: map[string]int{"o/r": 3}, Running: 1, Paused: 1, Concurrency: 2,
			Quotas: map[string]string{"claude": "week 78% left"},
			Jobs: []JobSnapshot{
				{Target: "o/r", Number: 7, Title: "Stacked fix", Status: "paused", WaitReason: "its stacked pull request waits on #12", PullRequest: 15, Log: "parked on #12"},
				{Target: "o/r", Number: 8, Title: "Running fix", Status: "active", Agent: "codex", Log: "running tests"},
			},
		},
		Settings: core.SettingsSnapshot{ConfiguredAgents: []string{"claude/opus"}, UnblockInterval: "15m", PromptAgent: "codex", PromptModel: "gpt-5.5"},
		Agents:   []string{"claude", "codex"},
		Logs:     []string{"stuck-job watchdog found 1 stuck job(s)"},
		Earlier:  []core.PromptEntry{{Request: "status?", Status: "done", Response: "all good", Actions: []string{"retry o/r#3: done"}}},
		Request:  "why is #7 paused?",
		Now:      time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC),
	})
	for _, want := range []string{
		"prompt agent of the glorp instance /glorp:ABCD1234",
		"Refuse anything else",
		"writing or editing code or files in any repository",
		"Never claim an issue, run /gh-fix",
		"verify its issue and pull request state with read-only gh commands now",
		"GLORP pause KEY",
		"GLORP resume KEY",
		"GLORP unblock all",
		"/gh-create OWNER/REPO <the owner's description> identity:/glorp:ABCD1234",
		"mentions its author as @LOGIN and this instance as @/glorp:ABCD1234",
		"Never edit an issue or pull request body or title to perform an update",
		"glorp version: v9.9.9",
		"o/r (3 issues)",
		"claude: week 78% left",
		"registered agents: claude, codex",
		"prompt agent: codex with model gpt-5.5",
		"key: o/r#7",
		"waits on: its stacked pull request waits on #12",
		"pull request: https://github.com/o/r/pull/15",
		"parked on #12",
		"key: o/r#8",
		"agent: codex",
		"stuck-job watchdog found 1 stuck job(s)",
		"action: retry o/r#3: done",
		"The owner's request:\nwhy is #7 paused?",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q:\n%s", want, prompt)
		}
	}
}

// A request returns its pending entry before the agent answers, and the
// answer lands in the conversation once the call finishes, run with the
// prompt agent's override as it stands at that request (issue #801).
func TestSubmitPromptAnswersAsynchronouslyWithTheConfiguredModel(t *testing.T) {
	release := make(chan struct{})
	specs := make(chan agentSpec, 4)
	reporter := &promptReporter{updates: make(chan []core.PromptEntry, 16)}
	w := &Glorp{Registry: agents.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, UI: reporter, Out: &syncBuffer{}}
	w.promptRun = func(_ context.Context, spec agentSpec, prompt string) (string, error) {
		specs <- spec
		<-release
		return "Version v1, one job running.", nil
	}
	entry, err := w.SubmitPrompt(context.Background(), "  status?  ")
	if err != nil || entry.Status != "pending" || entry.Request != "status?" {
		t.Fatalf("SubmitPrompt = %+v, %v", entry, err)
	}
	if first := <-reporter.updates; len(first) != 1 || first[0].Status != "pending" {
		t.Fatalf("first report = %+v", first)
	}
	if spec := <-specs; spec.String() != "claude/opus" {
		t.Errorf("default prompt agent = %s, want the coding agent claude/opus", spec)
	}
	close(release)
	answered := awaitPrompt(t, w, entry.ID)
	if answered.Status != "done" || answered.Response != "Version v1, one job running." || answered.Agent != "claude/opus" {
		t.Fatalf("answer = %+v", answered)
	}
	w.SetPromptAgent(unblockConfig{Agent: "codex", Model: "gpt-5.5"})
	next, _ := w.SubmitPrompt(context.Background(), "again")
	if spec := <-specs; spec.String() != "codex/gpt-5.5" {
		t.Errorf("prompt agent after a model change = %s, want codex/gpt-5.5", spec)
	}
	awaitPrompt(t, w, next.ID)
	if _, err := w.SubmitPrompt(context.Background(), "   "); err == nil {
		t.Error("an empty request was accepted")
	}
}

// A failed call is shown as failed with what the agent wrote, and actions
// glorp cannot perform report why instead of doing anything.
func TestSubmitPromptReportsFailuresAndRefusedActions(t *testing.T) {
	w := &Glorp{Registry: agents.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Out: &syncBuffer{}}
	w.published.Store(&core.Snapshot{Jobs: []JobSnapshot{{Target: "o/r", Number: 7, Status: "active"}}})
	w.promptRun = func(context.Context, agentSpec, string) (string, error) {
		return "Resuming.\nGLORP resume o/r#7\nGLORP stop o/r#99", errors.New("exit status 1")
	}
	entry, _ := w.SubmitPrompt(context.Background(), "resume 7")
	answered := awaitPrompt(t, w, entry.ID)
	if answered.Status != "failed" || !strings.Contains(answered.Response, "Resuming.") || !strings.Contains(answered.Response, "exit status 1") {
		t.Errorf("failed answer = %+v", answered)
	}
	want := []string{"resume o/r#7 failed: the job is active, not paused", "stop o/r#99 failed: glorp has no such job"}
	if strings.Join(answered.Actions, "|") != strings.Join(want, "|") {
		t.Errorf("actions = %q, want %q", answered.Actions, want)
	}
}

// The prompt agent's unblock action runs a watchdog round now, through the
// watchdog's own loop, over every stuck job or a named one, even with the
// timed watchdog turned off (issue #801).
func TestPromptUnblockActionRunsAWatchdogRoundNow(t *testing.T) {
	calls := newUnblockCalls("UNBLOCK o/r#1 none: its blocker merged", "UNBLOCK o/r#2 comment: https://example.test/c a nudge")
	logs := &syncBuffer{}
	w := watchdogGlorp(calls, logs)
	w.SetUnblockInterval(0)
	list := &jobList{}
	list.set(
		JobSnapshot{Target: "o/r", Number: 1, Status: "paused", WaitReason: "its stacked pull request waits on #9"},
		JobSnapshot{Target: "o/r", Number: 2, Status: "active", Log: "busy"},
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.watchStuckJobs(ctx, list.get)
	if got := w.applyPromptAction(ctx, promptAction{Kind: "unblock", Key: "all"}, "", nil); !strings.Contains(got, "round requested") {
		t.Fatalf("unblock all = %q", got)
	}
	calls.await(t, 1)
	if prompt := calls.prompt(0); !strings.Contains(prompt, "key: o/r#1") || strings.Contains(prompt, "key: o/r#2") {
		t.Errorf("the round over every stuck job did not cover just the paused one:\n%s", prompt)
	}
	waitForLog(t, logs, "stuck-job watchdog: o/r#1 needed no action: its blocker merged")
	w.published.Store(&core.Snapshot{Jobs: list.get()})
	if got := w.applyPromptAction(ctx, promptAction{Kind: "unblock", Key: "o/r#2"}, "", nil); !strings.Contains(got, "round requested") {
		t.Fatalf("unblock o/r#2 = %q", got)
	}
	calls.await(t, 1)
	if prompt := calls.prompt(1); !strings.Contains(prompt, "key: o/r#2") || !strings.Contains(prompt, "named from the prompt area while active") || strings.Contains(prompt, "key: o/r#1") {
		t.Errorf("the round over a named job did not cover just that job:\n%s", prompt)
	}
	w.applyPromptAction(ctx, promptAction{Kind: "unblock", Key: "o/r#404"}, "", nil)
	waitForLog(t, logs, "the unblock round requested for o/r#404 cannot run: glorp has no such job")
	if calls.count() != 2 {
		t.Errorf("a round for a missing job made an unblock call")
	}
}

func TestRequestedStuckJobsSkipsJobsSilentForTooShort(t *testing.T) {
	jobs := []JobSnapshot{{Target: "o/r", Number: 3, Status: "active", Log: "same"}}
	baseline := map[string]string{"o/r#3": "same"}
	if stuck, _ := requestedStuckJobs(jobs, baseline, map[string][]string{}, 5*time.Second, ""); len(stuck) != 0 {
		t.Errorf("a job silent for 5s was stuck: %+v", stuck)
	}
	if stuck, _ := requestedStuckJobs(jobs, baseline, map[string][]string{}, 10*time.Minute, ""); len(stuck) != 1 {
		t.Errorf("a job silent for 10m was not stuck: %+v", stuck)
	}
}

// From the prompt area, a job is paused and resumed with exactly what the
// dashboards' Pause and Retry buttons do: pausing stops its agent and holds it
// as paused by owner, and resuming requeues it with its session (issue #801).
func TestPromptAgentPausesAndResumesAJob(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	src := pausingSource([]Issue{{Number: 7, Title: "7", Repository: "o/r"}})
	src.states[7] = OriginatingWorkState{IssueState: "open"}
	runner := &pausableRunner{checkout: t.TempDir(), sessions: make(chan Issue, 4), exit: make(chan struct{})}
	close(runner.exit)
	logs := &syncBuffer{}
	w := &Glorp{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: statePath, Registry: agents.MustBuiltin(),
		Issues: src, Runner: outputPausableRunner{runner}, Out: logs, UI: &snapshotReporter{}, closureInterval: time.Hour, checkpointGit: (&recordingGit{}).run,
	}
	w.SetPromptAgent(unblockConfig{Agent: "claude"})
	replies := make(chan string, 2)
	w.promptRun = func(context.Context, agentSpec, string) (string, error) { return <-replies, nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		<-done
	}()
	select {
	case <-runner.sessions:
	case <-time.After(5 * time.Second):
		t.Fatalf("issue was not dispatched:\n%s", logs)
	}

	replies <- "Pausing it.\nGLORP pause o/r#7"
	entry, _ := w.SubmitPrompt(ctx, "pause 7")
	if answered := awaitPrompt(t, w, entry.ID); strings.Join(answered.Actions, "|") != "pause o/r#7: done" {
		t.Fatalf("pause answer = %+v\n%s", answered, logs)
	}
	waitForPaused(t, statePath, 7, core.PausedManual)
	deadline := time.Now().Add(5 * time.Second)
	for job, _ := w.publishedJob("o/r#7"); job.Status != "paused"; job, _ = w.publishedJob("o/r#7") {
		if time.Now().After(deadline) {
			t.Fatalf("the paused job was never published as paused: %+v", job)
		}
		time.Sleep(time.Millisecond)
	}

	replies <- "Resuming it.\nGLORP resume o/r#7"
	entry, _ = w.SubmitPrompt(ctx, "resume 7")
	if answered := awaitPrompt(t, w, entry.ID); strings.Join(answered.Actions, "|") != "resume o/r#7: done" {
		t.Fatalf("resume answer = %+v\n%s", answered, logs)
	}
	select {
	case issue := <-runner.sessions:
		if issue.Number != 7 {
			t.Fatalf("resume dispatched %+v", issue)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("the resumed job was not dispatched again:\n%s", logs)
	}
}

func TestGlorpApplySettingsChangesThePromptAgent(t *testing.T) {
	dir := t.TempDir()
	w := &Glorp{Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(dir, "state"), Issues: &fakeSource{batches: [][]Issue{{}}}, Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Registry: agents.MustBuiltin(), Out: &syncBuffer{}}
	ctx, cancel := context.WithCancel(context.Background())
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
	if snapshot.PromptAgent != "" || snapshot.PromptModel != "" || snapshot.PromptDefault != "claude/opus" {
		t.Fatalf("default prompt settings = %+v", snapshot)
	}
	agent, model := "codex", "gpt-5.5"
	if snapshot, err = w.ApplySettings(ctx, SettingsUpdate{PromptAgent: &agent, PromptModel: &model}); err != nil {
		t.Fatal(err)
	}
	if snapshot.PromptAgent != "codex" || snapshot.PromptModel != "gpt-5.5" {
		t.Fatalf("prompt settings after update = %+v", snapshot)
	}
	if spec, _ := w.promptSpec(""); spec.String() != "codex/gpt-5.5" {
		t.Fatalf("prompt spec after update = %s", spec)
	}
	unknown := "nope"
	if _, err := w.ApplySettings(ctx, SettingsUpdate{PromptAgent: &unknown}); err == nil {
		t.Error("an unknown prompt agent was accepted")
	}
}

// outputPausableRunner is a pausableRunner for a run with a dashboard, which
// hands the agent its output writer.
type outputPausableRunner struct{ *pausableRunner }

func (r outputPausableRunner) RunSessionWithOutput(ctx context.Context, issue Issue, session AgentSession, update func(AgentSession), _ io.Writer) error {
	return r.RunSession(ctx, issue, session, update)
}
