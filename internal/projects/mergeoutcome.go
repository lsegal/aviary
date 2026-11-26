package projects

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// mergeOutcomeTimeout bounds reading a merge job's pull request back once its
// agent exits, so a slow GitHub cannot hold its slot.
const mergeOutcomeTimeout = 30 * time.Second

// mergeJobPullRequest reads back the pull request a merge job worked on, so
// the job ends in the state GitHub reports rather than the one its agent
// process exited with (issue #1426). It reports false when the engine's
// source cannot read pull requests or GitHub did not answer.
func (w *Engine) mergeJobPullRequest(ctx context.Context, item queuedJob) (PullRequestWorkState, bool) {
	reader, ok := w.Issues.(PullRequestStateReader)
	if !ok || item.kind() != queuedKind(SkillSafeMerge) {
		return PullRequestWorkState{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, mergeOutcomeTimeout)
	defer cancel()
	pullRequest, err := reader.PullRequestState(ctx, item.Repository, item.Number)
	if err != nil {
		w.logf("%s %s: reading its pull request after the run: %v", item.kind(), item.key(), err)
		return PullRequestWorkState{}, false
	}
	return pullRequest, true
}

// settleMergeJob records a finished merge job's pull request on its card, so
// its PR state and stepper follow GitHub, and lets a merged pull request
// outrank the agent's exit: a run whose agent died or exited uncleanly after
// the merge landed is complete, with a note saying so, rather than failed. A
// run that exited cleanly but left its pull request open and unmerged, such
// as one stopped by a conflict it could not resolve after another merge
// landed first (issue #1433), fails so its card asks for attention. Any other
// pull request that did not merge leaves the outcome as it was.
func settleMergeJob(job *JobSnapshot, item queuedJob, pullRequest PullRequestWorkState, result projectTaskResult) projectTaskResult {
	job.SetPullRequest(OriginatingWorkState{PullRequests: []PullRequestWorkState{pullRequest}}, item.Repository)
	if !pullRequest.Merged && result.err == nil && strings.EqualFold(pullRequest.State, "open") {
		reason := fmt.Sprintf("pull request #%d is still open and was not merged", item.Number)
		if result.summary != "" {
			reason += ": " + result.summary
		}
		return projectTaskResult{summary: reason, err: errors.New(reason)}
	}
	if !pullRequest.Merged || result.err == nil {
		return result
	}
	reason := result.summary
	if reason == "" {
		reason = result.err.Error()
	}
	if errors.Is(result.err, context.Canceled) {
		return projectTaskResult{summary: fmt.Sprintf("pull request #%d merged; the run was stopped after it finished: %s", item.Number, reason)}
	}
	return projectTaskResult{summary: fmt.Sprintf("pull request #%d merged; the agent exited uncleanly after finishing: %s", item.Number, reason)}
}
