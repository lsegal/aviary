package projects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/lsegal/aviary/internal/projects/core"
)

// pullRequestSource is an issue source that also reads pull requests back,
// as GHCLI does.
type pullRequestSource struct {
	*fakeSource
	state PullRequestWorkState
}

func (s pullRequestSource) PullRequestState(_ context.Context, repo string, number int) (PullRequestWorkState, error) {
	state := s.state
	state.Repository, state.Number = repo, number
	return state, nil
}

// errWaitDelayExit is how an agent killed after its work landed exits.
var errWaitDelayExit = errors.New("agent process didn't exit cleanly: WaitDelay expired: exec: WaitDelay expired before I/O complete")

// A merge job whose agent dies after `gh pr merge` succeeded is complete,
// with a note that the agent exited uncleanly, not failed, and its card
// shows the merged pull request so the stepper reads done (issue #1426).
func TestMergeJobWhosePullRequestMergedCompletesDespiteUncleanExit(t *testing.T) {
	runner := newQueueRunner()
	source := pullRequestSource{fakeSource: &fakeSource{batches: [][]Issue{{}}}, state: PullRequestWorkState{State: "closed", Merged: true}}
	w, _ := startQueueEngineWith(t, runner, t.TempDir(), 1, source)
	queueJob(t, w, SkillSafeMerge, 1700)
	waitStarted(t, runner)
	runner.finish <- errWaitDelayExit
	job := waitJob(t, w, 1700, func(j JobSnapshot) bool { return j.Status == "complete" || j.Status == "failed" })
	if job.Status != "complete" || job.BoardState() != core.BoardDone {
		t.Fatalf("merged job status = %q (%s); want complete", job.Status, job.Summary)
	}
	if !strings.Contains(job.Summary, "pull request #1700 merged") || !strings.Contains(job.Summary, "exited uncleanly after finishing") {
		t.Fatalf("merged job summary = %q", job.Summary)
	}
	if job.PullRequest != 1700 || job.PullRequestState != core.PullRequestMerged {
		t.Fatalf("merged job pull request = #%d %q", job.PullRequest, job.PullRequestState)
	}
}

// A merge job whose agent died before its pull request merged still fails,
// and its card shows the pull request as still open.
func TestMergeJobWhosePullRequestDidNotMergeStillFails(t *testing.T) {
	runner := newQueueRunner()
	source := pullRequestSource{fakeSource: &fakeSource{batches: [][]Issue{{}}}, state: PullRequestWorkState{State: "open"}}
	w, _ := startQueueEngineWith(t, runner, t.TempDir(), 1, source)
	queueJob(t, w, SkillSafeMerge, 9)
	waitStarted(t, runner)
	runner.finish <- errWaitDelayExit
	job := waitJob(t, w, 9, func(j JobSnapshot) bool { return j.Status == "complete" || j.Status == "failed" })
	if job.Status != "failed" || !strings.Contains(job.Summary, "WaitDelay") {
		t.Fatalf("unmerged job = %q (%s); want failed", job.Status, job.Summary)
	}
	if job.PullRequest != 9 || job.PullRequestState != core.PullRequestOpen {
		t.Fatalf("unmerged job pull request = #%d %q", job.PullRequest, job.PullRequestState)
	}
}

// A merge job whose agent exits cleanly but leaves its pull request open,
// such as after a conflict it could not resolve once another merge landed
// first, fails so its card needs attention instead of reporting success
// (issue #1433).
func TestMergeJobLeavingItsPullRequestOpenNeedsAttention(t *testing.T) {
	runner := newQueueRunner()
	source := pullRequestSource{fakeSource: &fakeSource{batches: [][]Issue{{}}}, state: PullRequestWorkState{State: "open"}}
	w, _ := startQueueEngineWith(t, runner, t.TempDir(), 1, source)
	queueJob(t, w, SkillSafeMerge, 12)
	waitStarted(t, runner)
	runner.finish <- nil
	job := waitJob(t, w, 12, func(j JobSnapshot) bool { return j.Status == "complete" || j.Status == "failed" })
	if job.Status != "failed" || job.BoardState() != core.BoardFailed {
		t.Fatalf("job = %q (%s); want failed", job.Status, job.Summary)
	}
	if !strings.HasPrefix(job.Summary, "pull request #12 is still open and was not merged") {
		t.Fatalf("summary = %q", job.Summary)
	}
	if actions := core.JobActionsFor(job); !actions.Retry {
		t.Fatalf("actions = %+v; want retry", actions)
	}
}

func TestSettleMergeJob(t *testing.T) {
	item := queuedJob{Skill: SkillSafeMerge, Target: "o/r", Repository: "o/r", Number: 4}
	merged := PullRequestWorkState{Repository: "o/r", Number: 4, State: "closed", Merged: true}
	for _, test := range []struct {
		name        string
		pullRequest PullRequestWorkState
		result      projectTaskResult
		failed      bool
		summary     string
	}{
		{"clean run stays as it was", merged, projectTaskResult{summary: "Merged"}, false, "Merged"},
		{"merged after an unclean exit", merged, projectTaskResult{summary: "boom", err: fmt.Errorf("boom")}, false, "pull request #4 merged; the agent exited uncleanly after finishing: boom"},
		{"merged before the owner stopped it", merged, projectTaskOutcome(nil, errWorkStoppedFromWebUI, ""), false, "pull request #4 merged; the run was stopped after it finished: stopped by the owner"},
		{"unmerged failure stays failed", PullRequestWorkState{Repository: "o/r", Number: 4, State: "open"}, projectTaskResult{summary: "boom", err: fmt.Errorf("boom")}, true, "boom"},
		{"clean run leaving it open needs attention", PullRequestWorkState{Repository: "o/r", Number: 4, State: "open"}, projectTaskResult{summary: "Conflict in go.mod I could not resolve"}, true, "pull request #4 is still open and was not merged: Conflict in go.mod I could not resolve"},
		{"clean run that closed it stays as it was", PullRequestWorkState{Repository: "o/r", Number: 4, State: "closed"}, projectTaskResult{summary: "Closed as malicious"}, false, "Closed as malicious"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var job JobSnapshot
			got := settleMergeJob(&job, item, test.pullRequest, test.result)
			if (got.err != nil) != test.failed || got.summary != test.summary {
				t.Fatalf("settleMergeJob = %q, err %v; want %q, failed %v", got.summary, got.err, test.summary, test.failed)
			}
			// A closed, unmerged pull request is no longer the card's.
			if open := test.pullRequest.Merged || test.pullRequest.State == "open"; open && job.PullRequest != 4 {
				t.Fatalf("job pull request = %d", job.PullRequest)
			}
		})
	}
}

// GHCLI reads a pull request back by its own number.
func TestGHCLIPullRequestState(t *testing.T) {
	gh := GHCLI{publicRepoCache: &sync.Map{}, runCommand: func(_ context.Context, args ...string) ([]byte, error) {
		path := strings.Join(args, " ")
		switch {
		case strings.Contains(path, "repos/o/r/pulls/1700"):
			return []byte(`{"state":"closed","merged_at":"2026-11-26T19:51:39Z","merge_commit_sha":"8493187","draft":false,"body":"- [x] a\n- [ ] b","head":{"sha":"abc"},"base":{"ref":"main","repo":{"default_branch":"main"}}}`), nil
		case strings.Contains(path, "repos/o/r"):
			return []byte(`{"private":true}`), nil
		}
		return nil, fmt.Errorf("unexpected gh %s", path)
	}}
	state, err := gh.PullRequestState(context.Background(), "o/r", 1700)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Merged || state.Number != 1700 || state.Repository != "o/r" || state.MergeCommitSHA != "8493187" || state.HeadSHA != "abc" || state.TasksDone != 1 || state.TasksTotal != 2 || state.Stacked {
		t.Fatalf("PullRequestState = %+v", state)
	}
}
