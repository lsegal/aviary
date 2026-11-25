package projects

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

func TestQueueTarget(t *testing.T) {
	for _, c := range []struct {
		name    string
		engine  *Engine
		target  string
		want    string
		wantErr string
	}{
		{"only repo by default", &Engine{Repo: "o/r"}, "", "o/r", ""},
		{"only target by default", &Engine{Targets: []string{"o/a"}}, "  ", "o/a", ""},
		{"named target trimmed", &Engine{Targets: []string{"o/a", "o/b"}}, " o/b ", "o/b", ""},
		{"targets win over repo", &Engine{Repo: "o/r", Targets: []string{"o/a"}}, "o/r", "", "o/r is not watched"},
		{"several targets need a name", &Engine{Targets: []string{"o/a", "o/b"}}, "", "", "name the project to queue on"},
		{"no targets need a name", &Engine{}, "", "", "name the project to queue on"},
		{"unwatched target", &Engine{Repo: "o/r"}, "o/x", "", "o/x is not watched"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := queueTarget(c.engine, c.target)
			if c.wantErr != "" {
				if err == nil || err.Error() != c.wantErr {
					t.Fatalf("queueTarget = %q, %v; want error %q", got, err, c.wantErr)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("queueTarget = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestQueueAPIWithoutEngine(t *testing.T) {
	m := newTestManager(t)
	if _, err := m.QueueJobs(context.Background(), "o/r", "", SkillSafeMerge, []int{1}, nil); !errors.Is(err, ErrNotWatching) {
		t.Fatalf("QueueJobs error = %v, want ErrNotWatching", err)
	}
	if _, err := m.QueueCandidates(context.Background(), "o/r", "", SkillSafeMerge); !errors.Is(err, ErrNotWatching) {
		t.Fatalf("QueueCandidates error = %v, want ErrNotWatching", err)
	}
}

func TestQueueAPIRejectsUnwatchedTarget(t *testing.T) {
	m := newTestManager(t)
	m.running = &engineRun{engine: &Engine{Targets: []string{"o/a", "o/b"}}}
	for _, skill := range []string{SkillSafeMerge, SkillFix} {
		if _, err := m.QueueJobs(context.Background(), "o/x", "", skill, []int{1}, nil); err == nil || err.Error() != "o/x is not watched" {
			t.Fatalf("QueueJobs %s error = %v", skill, err)
		}
	}
	if _, err := m.QueueJobs(context.Background(), "", "", SkillFix, []int{1}, nil); err == nil || err.Error() != "name the project to queue on" {
		t.Fatalf("QueueJobs with no target error = %v", err)
	}
	if _, err := m.QueueCandidates(context.Background(), "o/x", "", SkillSafeMerge); err == nil || err.Error() != "o/x is not watched" {
		t.Fatalf("QueueCandidates error = %v", err)
	}
}

func TestManagerQueueJobsQueuesWithTitles(t *testing.T) {
	runner := newQueueRunner()
	w, _ := startQueueEngine(t, runner, t.TempDir(), 1)
	m := newTestManager(t)
	m.running = &engineRun{engine: w}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := m.QueueJobs(ctx, "", "", SkillSafeMerge, []int{12}, map[int]string{12: "Fix the parser"})
	if err != nil {
		t.Fatal(err)
	}
	if result["target"] != "o/r" || result["skill"] != SkillSafeMerge {
		t.Fatalf("result = %+v", result)
	}
	outcomes, _ := result["results"].([]QueueOutcome)
	if len(outcomes) != 1 || outcomes[0].Number != 12 || !outcomes[0].Queued {
		t.Fatalf("results = %+v", result["results"])
	}
	waitStarted(t, runner)
	if job := waitJob(t, w, 12, func(JobSnapshot) bool { return true }); job.Title != "Fix the parser" {
		t.Fatalf("queued job title = %q", job.Title)
	}
	runner.finish <- nil
	waitJob(t, w, 12, func(j JobSnapshot) bool { return j.Status == "complete" })
}

func TestManagerQueueJobsReportsEngineErrors(t *testing.T) {
	m := newTestManager(t)
	engine := &Engine{Repo: "o/r", notReadyWait: 10 * time.Millisecond}
	m.running = &engineRun{engine: engine}
	if _, err := m.QueueJobs(context.Background(), "o/r", "", "gh-other", []int{1}, nil); err == nil || !strings.Contains(err.Error(), `skill "gh-other" cannot be queued`) {
		t.Fatalf("unknown skill error = %v", err)
	}
	// An engine that has not reached its dispatch loop asks for a retry
	// rather than reporting the internal sentinel.
	_, err := m.QueueJobs(context.Background(), "o/r", "", SkillDiscuss, []int{1}, nil)
	if err == nil || err.Error() != "the engine is still starting; try again" {
		t.Fatalf("not ready error = %v", err)
	}
}

// fakeJobActions stands in for a running engine's dispatch loop: it answers
// each job action with fail's result for the job, recording what it saw.
func fakeJobActions(t *testing.T, engine *Engine, fail func(core.JobAction) error) <-chan core.JobAction {
	t.Helper()
	close(engine.readyChan())
	seen := make(chan core.JobAction, 8)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		for {
			select {
			case request := <-engine.jobActionRequests():
				seen <- request.action
				request.done <- fail(request.action)
			case <-ctx.Done():
				return
			}
		}
	}()
	return seen
}

func TestManagerQueueFixes(t *testing.T) {
	m := newTestManager(t)
	engine := &Engine{Repo: "o/r"}
	m.running = &engineRun{engine: engine}
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 1, Target: "o/r", Kind: core.JobKindIssue, Status: "active"},
		{Number: 2, Target: "o/r", Kind: core.JobKindIssue, Status: "complete"},
		{Number: 3, Target: "o/r", Kind: core.JobKindIssue, Status: "failed"},
		{Number: 4, Target: "o/r", Kind: core.JobKindDiscussion, Status: "complete"},
		{Number: 7, Target: "o/other", Kind: core.JobKindIssue, Status: "active"},
	}})
	seen := fakeJobActions(t, engine, func(action core.JobAction) error {
		if action.Number == 3 {
			return errors.New("issue is closed")
		}
		return nil
	})

	if _, err := m.QueueJobs(context.Background(), "o/r", "", SkillFix, nil, nil); err == nil || err.Error() != "name at least one issue to fix" {
		t.Fatalf("no numbers error = %v", err)
	}

	result, err := m.QueueJobs(context.Background(), "o/r", "", SkillFix, []int{1, 2, 3, 4, 5, 7}, map[int]string{5: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if result["target"] != "o/r" || result["skill"] != SkillFix {
		t.Fatalf("result = %+v", result)
	}
	noJob := "no job yet; checking GitHub now, which dispatches it if it matches the project's issue filter"
	want := []QueueOutcome{
		{Number: 1, OwnedBy: "o/r#1", Reason: "already owned by job o/r#1"},
		{Number: 2, Key: "o/r#2", Queued: true, Reason: "retried its finished job"},
		{Number: 3, Reason: "issue is closed"},
		{Number: 4, OwnedBy: "o/r#4", Reason: "job o/r#4 is a " + core.JobKindDiscussion + " job"},
		{Number: 5, Reason: noJob},
		// A job on another project does not own this project's number.
		{Number: 7, Reason: noJob},
	}
	if got, _ := result["results"].([]QueueOutcome); !reflect.DeepEqual(got, want) {
		t.Fatalf("results = %+v\nwant %+v", got, want)
	}

	// Only the finished issue jobs were retried, through the engine.
	for _, number := range []int{2, 3} {
		select {
		case action := <-seen:
			if action != (core.JobAction{Action: "retry", Target: "o/r", Number: number}) {
				t.Fatalf("job action = %+v, want retry of #%d", action, number)
			}
		default:
			t.Fatalf("issue #%d was not retried", number)
		}
	}
	select {
	case action := <-seen:
		t.Fatalf("unexpected job action %+v", action)
	default:
	}

	// The numbers with no job share one poll.
	if got := len(engine.negotiatedNudges()); got != 1 {
		t.Fatalf("%d polls nudged, want 1", got)
	}
}

func TestManagerQueueFixesSkipsPollWhenEveryIssueHasAJob(t *testing.T) {
	m := newTestManager(t)
	engine := &Engine{Repo: "o/r"}
	m.running = &engineRun{engine: engine}
	m.snapshot.Store(&core.Snapshot{Jobs: []core.JobSnapshot{
		{Number: 1, Target: "o/r", Kind: core.JobKindIssue, Status: "waiting"},
	}})
	if _, err := m.QueueJobs(context.Background(), "", "", SkillFix, []int{1}, nil); err != nil {
		t.Fatal(err)
	}
	if got := len(engine.negotiatedNudges()); got != 0 {
		t.Fatalf("%d polls nudged, want none", got)
	}
}

func TestManagerQueueCandidatesUsesEngine(t *testing.T) {
	m := newTestManager(t)
	m.running = &engineRun{engine: browserDeliveryEngine(func(_ context.Context, args ...string) ([]byte, error) {
		return []byte(`[{"number":7,"title":"Fix it","url":"u","author":{"login":"alice"}}]`), nil
	})}
	result, err := m.QueueCandidates(context.Background(), " ", "", SkillSafeMerge)
	if err != nil {
		t.Fatal(err)
	}
	pulls, _ := result["pull_requests"].([]OpenPullRequest)
	if len(pulls) != 1 || pulls[0].Number != 7 || pulls[0].Title != "Fix it" || pulls[0].Author != "alice" {
		t.Fatalf("pull_requests = %+v", result["pull_requests"])
	}
}
