package projects

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// queueRunner reports each run it starts and holds it until the test sends
// the run's result on finish.
type queueRunner struct {
	started chan Issue
	finish  chan error
}

func newQueueRunner() *queueRunner {
	return &queueRunner{started: make(chan Issue, 8), finish: make(chan error)}
}

func (r *queueRunner) AgentName() string                { return "codex" }
func (r *queueRunner) Run(context.Context, Issue) error { return nil }
func (r *queueRunner) RunWithOutput(ctx context.Context, issue Issue, output io.Writer) error {
	r.started <- issue
	select {
	case err := <-r.finish:
		if err != nil {
			_, _ = io.WriteString(output, "Skipped: the pull request conflicts with main\n")
			return err
		}
		_, _ = io.WriteString(output, "Merged the pull request\n")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startQueueEngine runs an engine with no issues to dispatch, saving its
// state under dir, and returns a function that stops it.
func startQueueEngine(t *testing.T, runner *queueRunner, dir string, concurrency int) (*Engine, func()) {
	t.Helper()
	return startQueueEngineWith(t, runner, dir, concurrency, &fakeSource{batches: [][]Issue{{}}})
}

// startQueueEngineWith is startQueueEngine reading issues from source.
func startQueueEngineWith(t *testing.T, runner *queueRunner, dir string, concurrency int, source IssueSource) (*Engine, func()) {
	t.Helper()
	w := &Engine{
		Repo: "o/r", Interval: time.Hour, Concurrency: concurrency,
		StatePath: filepath.Join(dir, "state.json"),
		Issues:    source,
		Runner:    runner, UI: &snapshotReporter{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(stop)
	return w, stop
}

func queueJob(t *testing.T, w *Engine, skill string, numbers ...int) []QueueOutcome {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	outcomes, err := w.QueueJobs(ctx, QueueRequest{Target: "o/r", Skill: skill, Numbers: numbers})
	if err != nil {
		t.Fatal(err)
	}
	return outcomes
}

func waitStarted(t *testing.T, runner *queueRunner) Issue {
	t.Helper()
	select {
	case issue := <-runner.started:
		return issue
	case <-time.After(5 * time.Second):
		t.Fatal("no queued job started")
	}
	return Issue{}
}

func waitJob(t *testing.T, w *Engine, number int, ok func(JobSnapshot) bool) JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		job, found := scheduledJob(w, number)
		if found && ok(job) {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("job #%d = %+v (found %v)", number, job, found)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func queueAction(t *testing.T, w *Engine, action string, number int) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return w.handleJobAction(ctx, core.JobAction{Action: action, Target: "o/r", Number: number})
}

func TestQueuedMergesRunSeriallyInOrder(t *testing.T) {
	runner := newQueueRunner()
	w, _ := startQueueEngine(t, runner, t.TempDir(), 3)

	outcomes := queueJob(t, w, SkillSafeMerge, 1201, 1207)
	if len(outcomes) != 2 || !outcomes[0].Queued || !outcomes[1].Queued {
		t.Fatalf("outcomes = %+v", outcomes)
	}
	first := waitStarted(t, runner)
	if first.Number != 1201 || first.Skill != SkillSafeMerge || first.Repository != "o/r" {
		t.Fatalf("first run = %+v", first)
	}
	queued := waitJob(t, w, 1207, func(j JobSnapshot) bool { return j.Status == "queued" })
	if queued.Kind != core.JobKindMerge || queued.QueuePosition != 2 || queued.PullRequest != 1207 {
		t.Fatalf("second merge = %+v", queued)
	}
	// Free slots do not start the second merge while the first runs.
	select {
	case issue := <-runner.started:
		t.Fatalf("#%d started beside the running merge", issue.Number)
	case <-time.After(100 * time.Millisecond):
	}
	runner.finish <- nil
	second := waitStarted(t, runner)
	if second.Number != 1207 {
		t.Fatalf("second run = %+v", second)
	}
	done := waitJob(t, w, 1201, func(j JobSnapshot) bool { return j.Status == "complete" })
	if done.Summary != "Merged the pull request" || done.QueuePosition != 0 {
		t.Fatalf("finished merge = %+v", done)
	}
	runner.finish <- nil
	waitJob(t, w, 1207, func(j JobSnapshot) bool { return j.Status == "complete" })
}

func TestQueuedDiscussionsRunInParallel(t *testing.T) {
	runner := newQueueRunner()
	w, _ := startQueueEngine(t, runner, t.TempDir(), 2)
	queueJob(t, w, SkillDiscuss, 4, 9)
	got := map[int]bool{waitStarted(t, runner).Number: true, waitStarted(t, runner).Number: true}
	if !got[4] || !got[9] {
		t.Fatalf("started %v, want both discussions", got)
	}
	job := waitJob(t, w, 4, func(j JobSnapshot) bool { return j.Status == "active" })
	if job.Kind != core.JobKindDiscuss || job.IssueURL() != "https://github.com/o/r/discussions/4" {
		t.Fatalf("discuss job = %+v, url %s", job, job.IssueURL())
	}
	runner.finish <- nil
	runner.finish <- nil
}

func TestQueueRespectsConcurrency(t *testing.T) {
	runner := newQueueRunner()
	w, _ := startQueueEngine(t, runner, t.TempDir(), 1)
	queueJob(t, w, SkillDiscuss, 4, 9)
	waitStarted(t, runner)
	select {
	case issue := <-runner.started:
		t.Fatalf("#%d started past the concurrency limit", issue.Number)
	case <-time.After(100 * time.Millisecond):
	}
	runner.finish <- nil
	waitStarted(t, runner)
	runner.finish <- nil
}

func TestQueueDedupesOwnedNumbers(t *testing.T) {
	runner := newQueueRunner()
	w, _ := startQueueEngine(t, runner, t.TempDir(), 1)
	queueJob(t, w, SkillSafeMerge, 1201)
	waitStarted(t, runner)
	outcomes := queueJob(t, w, SkillSafeMerge, 1201, 1300)
	if outcomes[0].Queued || outcomes[0].OwnedBy != "o/r#1201" {
		t.Fatalf("requeueing a running merge = %+v", outcomes[0])
	}
	if !outcomes[1].Queued {
		t.Fatalf("a new pull request was not queued: %+v", outcomes[1])
	}
	runner.finish <- nil
	waitStarted(t, runner)
	runner.finish <- nil
}

func TestQueuedJobOwnerFindsFixJobsPullRequest(t *testing.T) {
	jobs := map[string]JobSnapshot{
		"o/r#50": {Kind: core.JobKindIssue, Target: "o/r", Number: 50, Status: "active", PullRequest: 51},
		"o/r#60": {Kind: core.JobKindIssue, Target: "o/r", Number: 60, Status: "complete", PullRequest: 61},
		"o/r#70": {Kind: core.JobKindDiscussion, Target: "o/r", Number: 70, Status: "active"},
	}
	merge := func(n int) queuedJob {
		return queuedJob{Skill: SkillSafeMerge, Target: "o/r", Repository: "o/r", Number: n}
	}
	if owner := queuedJobOwner(jobs, merge(51)); owner != "o/r#50" {
		t.Fatalf("an active fix job's pull request is owned by %q", owner)
	}
	if owner := queuedJobOwner(jobs, merge(61)); owner != "" {
		t.Fatalf("a finished fix job still owns its pull request: %q", owner)
	}
	if owner := queuedJobOwner(jobs, queuedJob{Skill: SkillDiscuss, Target: "o/r", Repository: "o/r", Number: 70}); owner != "o/r#70" {
		t.Fatalf("a Discussions board job's thread is owned by %q", owner)
	}
}

func TestQueueRefusesSafeMergeWithNoMerge(t *testing.T) {
	runner := newQueueRunner()
	w, _ := startQueueEngine(t, runner, t.TempDir(), 1)
	w.noMerge.Store(true)
	_, err := w.QueueJobs(context.Background(), QueueRequest{Target: "o/r", Skill: SkillSafeMerge, Numbers: []int{3}})
	if !errors.Is(err, ErrNoMergeQueue) {
		t.Fatalf("err = %v, want ErrNoMergeQueue", err)
	}
	if _, err := w.QueueJobs(context.Background(), QueueRequest{Target: "o/r", Skill: SkillFix, Numbers: []int{3}}); err == nil {
		t.Fatal("gh-fix reached the engine queue")
	}
	if _, err := w.QueueJobs(context.Background(), QueueRequest{Target: "x/y", Skill: SkillDiscuss, Numbers: []int{3}}); err == nil {
		t.Fatal("queued on a project that is not watched")
	}
}

func TestFailedMergeShowsItsReason(t *testing.T) {
	runner := newQueueRunner()
	w, _ := startQueueEngine(t, runner, t.TempDir(), 1)
	queueJob(t, w, SkillSafeMerge, 8)
	waitStarted(t, runner)
	runner.finish <- errors.New("exit status 1")
	job := waitJob(t, w, 8, func(j JobSnapshot) bool { return j.Status == "failed" })
	if job.Summary != "exit status 1" {
		t.Fatalf("failed merge summary = %q", job.Summary)
	}
	if !strings.Contains(job.Log, "conflicts with main") {
		t.Fatalf("failed merge log = %q", job.Log)
	}
	if actions := core.JobActionsFor(job); !actions.Retry || actions.Pause {
		t.Fatalf("failed merge actions = %+v", actions)
	}
	// Retry queues it again.
	if err := queueAction(t, w, "retry", 8); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, runner)
	runner.finish <- nil
	waitJob(t, w, 8, func(j JobSnapshot) bool { return j.Status == "complete" })
}

func TestPausedMergeKeepsItsPlaceAndStopRemovesQueued(t *testing.T) {
	runner := newQueueRunner()
	w, _ := startQueueEngine(t, runner, t.TempDir(), 1)
	queueJob(t, w, SkillSafeMerge, 1, 2, 3)
	waitStarted(t, runner)
	if err := queueAction(t, w, "pause", 2); err != nil {
		t.Fatal(err)
	}
	waitJob(t, w, 2, func(j JobSnapshot) bool { return j.Status == "paused" && j.QueuePosition == 0 })
	waitJob(t, w, 3, func(j JobSnapshot) bool { return j.QueuePosition == 2 })
	// Resumed, it is back in front of #3, which was queued after it.
	if err := queueAction(t, w, "retry", 2); err != nil {
		t.Fatal(err)
	}
	waitJob(t, w, 2, func(j JobSnapshot) bool { return j.Status == "queued" && j.QueuePosition == 2 })
	waitJob(t, w, 3, func(j JobSnapshot) bool { return j.QueuePosition == 3 })
	runner.finish <- nil
	if issue := waitStarted(t, runner); issue.Number != 2 {
		t.Fatalf("#%d ran after #1, want #2", issue.Number)
	}
	if err := queueAction(t, w, "stop", 3); err != nil {
		t.Fatal(err)
	}
	if _, found := scheduledJob(w, 3); found {
		t.Fatal("a stopped queued merge is still on the board")
	}
	// Pausing the running merge stops its agent and holds it; Retry runs it
	// again.
	if err := queueAction(t, w, "pause", 2); err != nil {
		t.Fatal(err)
	}
	waitJob(t, w, 2, func(j JobSnapshot) bool { return j.Status == "paused" })
	if err := queueAction(t, w, "retry", 2); err != nil {
		t.Fatal(err)
	}
	if issue := waitStarted(t, runner); issue.Number != 2 {
		t.Fatalf("resumed #%d, want #2", issue.Number)
	}
	// Stopping a running merge fails it with the reason.
	if err := queueAction(t, w, "stop", 2); err != nil {
		t.Fatal(err)
	}
	job := waitJob(t, w, 2, func(j JobSnapshot) bool { return j.Status == "failed" })
	if job.Summary != "stopped by the owner" {
		t.Fatalf("stopped merge summary = %q", job.Summary)
	}
}

func TestQueuedJobsSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	runner := newQueueRunner()
	w, stop := startQueueEngine(t, runner, dir, 1)
	queueJob(t, w, SkillSafeMerge, 10, 11)
	waitStarted(t, runner)
	stop()
	if _, err := os.Stat(filepath.Join(dir, "queue.json")); err != nil {
		t.Fatalf("the queue was not saved: %v", err)
	}

	restarted := newQueueRunner()
	w, _ = startQueueEngine(t, restarted, dir, 1)
	if issue := waitStarted(t, restarted); issue.Number != 10 || issue.Skill != SkillSafeMerge {
		t.Fatalf("after the restart #%d (%s) ran first, want #10", issue.Number, issue.Skill)
	}
	waitJob(t, w, 11, func(j JobSnapshot) bool { return j.Status == "queued" && j.QueuePosition == 2 })
	restarted.finish <- nil
	waitStarted(t, restarted)
	restarted.finish <- nil
	waitJob(t, w, 11, func(j JobSnapshot) bool { return j.Status == "complete" })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(dir, "queue.json")); errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the emptied queue was not removed")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSafeMergePrompt(t *testing.T) {
	r := CommandRunner{Repo: "o/r", Identity: "ABCD1234", AllowedCommenters: []string{"owner"}}
	prompt := jobPrompt(r, Issue{Number: 1201, Target: "o/r", Skill: SkillSafeMerge}, AgentSession{RunDir: "/tmp/clones"})
	for _, want := range []string{"/gh-safe-merge o/r#1201 identity:/aviary:ABCD1234", "Allowed commenters: owner", "Clone directory: /tmp/clones"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	discuss := jobPrompt(r, Issue{Number: 4, Target: "o/r", Skill: SkillDiscuss}, AgentSession{RunDir: "/tmp/clones"})
	if !strings.HasPrefix(discuss, "/gh-discuss 4\n\nRepository: o/r") || strings.Contains(discuss, "identity:") || strings.Contains(discuss, "Clone directory") {
		t.Errorf("discuss prompt:\n%s", discuss)
	}
}

func TestRollupCI(t *testing.T) {
	for _, c := range []struct {
		checks []ghCheckState
		want   string
	}{
		{nil, "none"},
		{[]ghCheckState{{Status: "COMPLETED", Conclusion: "SUCCESS"}, {Status: "COMPLETED", Conclusion: "SKIPPED"}}, "passing"},
		{[]ghCheckState{{Status: "COMPLETED", Conclusion: "SUCCESS"}, {Status: "IN_PROGRESS"}}, "pending"},
		{[]ghCheckState{{Status: "IN_PROGRESS"}, {Status: "COMPLETED", Conclusion: "FAILURE"}}, "failing"},
		{[]ghCheckState{{State: "PENDING"}}, "pending"},
		{[]ghCheckState{{State: "ERROR"}}, "failing"},
	} {
		if got := rollupCI(c.checks); got != c.want {
			t.Errorf("rollupCI(%+v) = %q, want %q", c.checks, got, c.want)
		}
	}
}

func TestListOpenPullRequests(t *testing.T) {
	g := GHCLI{runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] != "pr" || args[1] != "list" {
			t.Fatalf("args = %v", args)
		}
		return []byte(`[{"number":7,"title":"Fix it","url":"https://github.com/o/r/pull/7","isDraft":false,"mergeable":"CONFLICTING","reviewDecision":"APPROVED","author":{"login":"alice"},"statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"SUCCESS"}]},
			{"number":8,"title":"WIP","url":"u","isDraft":true,"mergeable":"UNKNOWN","reviewDecision":"","author":{"login":"bob"},"statusCheckRollup":[]}]`), nil
	}}
	pulls, err := g.ListOpenPullRequests(context.Background(), "o/r")
	if err != nil {
		t.Fatal(err)
	}
	want := []OpenPullRequest{
		{Number: 7, Title: "Fix it", Author: "alice", URL: "https://github.com/o/r/pull/7", CI: "passing", Review: "approved", Mergeable: "conflicting"},
		{Number: 8, Title: "WIP", Author: "bob", URL: "u", Draft: true, CI: "none", Review: "none", Mergeable: "unknown"},
	}
	if len(pulls) != len(want) {
		t.Fatalf("pulls = %+v", pulls)
	}
	for i := range want {
		if pulls[i] != want[i] {
			t.Errorf("pull %d = %+v, want %+v", i, pulls[i], want[i])
		}
	}
}

func TestJobQueueEdits(t *testing.T) {
	t.Run("without a path it keeps jobs in memory", func(t *testing.T) {
		q := &jobQueue{}
		if err := q.add(queuedJob{Skill: SkillSafeMerge, Target: "o/r", Number: 1}); err != nil {
			t.Fatal(err)
		}
		if got := q.list(); len(got) != 1 || got[0].Number != 1 {
			t.Fatalf("list = %+v", got)
		}
	})
	t.Run("missing keys change nothing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "queue.json")
		q := &jobQueue{path: path}
		if err := q.add(queuedJob{Skill: SkillDiscuss, Target: "o/r", Number: 2}); err != nil {
			t.Fatal(err)
		}
		saved, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := q.remove("o/r#9"); err != nil {
			t.Fatal(err)
		}
		if err := q.setPaused("o/r#9", true); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(saved) {
			t.Fatalf("queue file changed from %s to %s", saved, after)
		}
		if got := q.list(); len(got) != 1 || got[0].Paused {
			t.Fatalf("list = %+v", got)
		}
	})
	t.Run("emptying it reports a file it cannot remove", func(t *testing.T) {
		// A non-empty directory in the queue file's place cannot be removed.
		path := filepath.Join(t.TempDir(), "queue.json")
		if err := os.MkdirAll(filepath.Join(path, "child"), 0o755); err != nil {
			t.Fatal(err)
		}
		q := &jobQueue{path: path, loaded: true, items: []queuedJob{{Skill: SkillSafeMerge, Target: "o/r", Number: 3}}}
		if err := q.remove("o/r#3"); err == nil {
			t.Fatal("removing the last job did not report the undeletable queue file")
		}
	})
}

func TestQueueJobsValidatesRequest(t *testing.T) {
	w := &Engine{Targets: []string{"o/r", "users/me/projects/1"}}
	for _, c := range []struct {
		name    string
		request QueueRequest
		want    string
	}{
		{"no repository for a project", QueueRequest{Target: "users/me/projects/1", Skill: SkillDiscuss, Numbers: []int{1}}, "name the repository"},
		{"no numbers", QueueRequest{Target: "o/r", Skill: SkillDiscuss}, "at least one number"},
		{"non-positive number", QueueRequest{Target: "o/r", Skill: SkillDiscuss, Numbers: []int{4, 0}}, "0 is not a pull request"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := w.QueueJobs(context.Background(), c.request)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
		})
	}
}

func TestQueuedJobOwner(t *testing.T) {
	merge := queuedJob{Skill: SkillSafeMerge, Target: "o/r", Repository: "o/r", Number: 7}
	discuss := queuedJob{Skill: SkillDiscuss, Target: "o/r", Repository: "o/r", Number: 7}
	for _, c := range []struct {
		name string
		jobs map[string]JobSnapshot
		item queuedJob
		want string
	}{
		{"finished jobs own nothing", map[string]JobSnapshot{"o/r#7": {Status: "complete"}}, merge, ""},
		{"another repository's pull request", map[string]JobSnapshot{"x/y#3": {Status: "active", Target: "x/y", PullRequest: 7}}, merge, ""},
		{"a fix job's pull request in another repository", map[string]JobSnapshot{"board#3": {Status: "active", Repository: "o/r", PullRequestRepository: "x/y", PullRequest: 7}}, merge, ""},
		{"a fix job's pull request", map[string]JobSnapshot{"board#3": {Status: "active", Repository: "O/R", PullRequestRepository: "o/r", PullRequest: 7}}, merge, "board#3"},
		{"a merge of the same number on another board", map[string]JobSnapshot{"board#7": {Status: "queued", Repository: "o/r", Kind: core.JobKindMerge, Number: 7}}, merge, "board#7"},
		{"a discussion job answering the thread", map[string]JobSnapshot{"board#7": {Status: "active", Repository: "o/r", Kind: core.JobKindDiscussion, Number: 7}}, discuss, "board#7"},
		{"an issue job with the thread's number", map[string]JobSnapshot{"board#7": {Status: "active", Repository: "o/r", Kind: core.JobKindIssue, Number: 7}}, discuss, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := queuedJobOwner(c.jobs, c.item); got != c.want {
				t.Fatalf("owner = %q, want %q", got, c.want)
			}
		})
	}
}

func TestQueuedJobActionsRefuseTheirStatus(t *testing.T) {
	runner := newQueueRunner()
	w, _ := startQueueEngine(t, runner, t.TempDir(), 1)
	queueJob(t, w, SkillSafeMerge, 1, 2)
	waitStarted(t, runner)
	waitJob(t, w, 2, func(j JobSnapshot) bool { return j.Status == "queued" })
	if err := queueAction(t, w, "retry", 2); err == nil || !strings.Contains(err.Error(), "Already queued") {
		t.Fatalf("retrying a queued merge: %v", err)
	}
	if err := queueAction(t, w, "archive", 2); err == nil || !strings.Contains(err.Error(), "merge jobs cannot be archive") {
		t.Fatalf("unknown action: %v", err)
	}
	runner.finish <- nil
	waitJob(t, w, 1, func(j JobSnapshot) bool { return j.Status == "complete" })
	for _, action := range []string{"pause", "stop"} {
		if err := queueAction(t, w, action, 1); err == nil || !strings.Contains(err.Error(), "while complete") {
			t.Fatalf("%s on a complete merge: %v", action, err)
		}
	}
	// With no_merge set, a finished merge is not queued again.
	w.noMerge.Store(true)
	if err := queueAction(t, w, "retry", 1); !errors.Is(err, ErrNoMergeQueue) {
		t.Fatalf("retry with no_merge = %v, want ErrNoMergeQueue", err)
	}
	if job, _ := scheduledJob(w, 1); job.Status != "complete" {
		t.Fatalf("refused retry left #1 %s", job.Status)
	}
}

func TestStoppingAPausedMergeTakesItOffTheBoard(t *testing.T) {
	runner := newQueueRunner()
	w, _ := startQueueEngine(t, runner, t.TempDir(), 1)
	queueJob(t, w, SkillSafeMerge, 1, 2)
	waitStarted(t, runner)
	if err := queueAction(t, w, "pause", 2); err != nil {
		t.Fatal(err)
	}
	waitJob(t, w, 2, func(j JobSnapshot) bool { return j.Status == "paused" })
	if err := queueAction(t, w, "stop", 2); err != nil {
		t.Fatal(err)
	}
	if _, found := scheduledJob(w, 2); found {
		t.Fatal("a stopped paused merge is still on the board")
	}
	for _, item := range w.jobQueue().list() {
		if item.Number == 2 {
			t.Fatalf("a stopped paused merge is still queued: %+v", item)
		}
	}
	runner.finish <- nil
}
