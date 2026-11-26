package projects

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/browser"
	"github.com/lsegal/aviary/internal/projects/core"
)

// manualTrackingRun is a manual-mode run under one delivery mode, with the
// pieces a test drives it through.
type manualTrackingRun struct {
	w        *Engine
	src      *manualSource
	runner   *fakeRunner
	comments *fakeCommentClient
	mentions *fakeMentionSource
	events   chan WebhookEvent
}

// startManualTrackingRun starts a run whose only project, o/r, is in manual
// mode, delivered by mode: "poll", "webhook" or "browser". Issue #99 matches
// the filter, so a watched project would pick it up.
func startManualTrackingRun(t *testing.T, mode string) *manualTrackingRun {
	t.Helper()
	src := newManualSource()
	src.fakeClosureSource.state = OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 8, State: "open"}}}
	run := &manualTrackingRun{
		src: src, runner: &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 8)},
		comments: newFakeCommentClient(), mentions: newFakeMentionSource(),
	}
	close(run.runner.release)
	logs := &syncBuffer{}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("engine log:\n%s", logs)
		}
	})
	w := &Engine{
		Repo: "o/r", Manual: core.NewManualTargets([]string{"o/r"}), Interval: 5 * time.Millisecond, Concurrency: 1,
		StatePath: filepath.Join(t.TempDir(), "state.json"), Issues: src, Runner: run.runner, Out: logs, UI: &snapshotReporter{},
		Identity: "SELF", AllowedCommenters: []string{"lsegal"}, Comments: run.comments, Mentions: run.mentions,
		closureInterval: time.Millisecond,
	}
	switch mode {
	case "webhook":
		run.events = make(chan WebhookEvent, 4)
		w.UseWebhooks, w.Events, w.fallbackInterval, w.reapInterval = true, run.events, time.Hour, 5*time.Millisecond
	case "browser":
		// Browser delivery reads pages for discovery and reads tracked
		// issues back through the API client, as ApplyBrowserSources wires
		// it.
		w.Issues = browser.WatchIssues{Repos: src, Work: src}
	}
	run.w = w
	runEngine(t, w)
	return run
}

// awaitDispatch waits for the run to dispatch number, failing on any other.
func (r *manualTrackingRun) awaitDispatch(t *testing.T, number int, why string) {
	t.Helper()
	select {
	case n := <-r.runner.dispatched:
		if n != number {
			t.Fatalf("%s dispatched #%d, want #%d", why, n, number)
		}
	case <-time.After(runLoopWait):
		t.Fatalf("%s never dispatched #%d", why, number)
	}
}

// awaitStatus waits for number's work to reach status.
func (r *manualTrackingRun) awaitStatus(t *testing.T, number int, status string) {
	t.Helper()
	waitFor(t, "the job to be "+status, func() bool {
		state, err := loadWorkState(r.w.StatePath)
		return err == nil && state[number].Status == status
	})
}

// settle lets the run poll several more times.
func (r *manualTrackingRun) settle(t *testing.T) {
	t.Helper()
	before := r.src.hydrateCount()
	waitFor(t, "more polls", func() bool { return r.src.hydrateCount() >= before+5 })
}

// expectNoDispatch fails if anything was dispatched.
func (r *manualTrackingRun) expectNoDispatch(t *testing.T, why string) {
	t.Helper()
	select {
	case n := <-r.runner.dispatched:
		t.Fatalf("%s dispatched #%d", why, n)
	default:
	}
}

// A manual project turns off only discovery (issue #1431). Under every
// delivery mode, a hand-queued job is picked up at once and then tracked like
// a watched project's: an allowed commenter's mention wakes it, its pull
// request being rejected restarts it, its merge finishes it, a matching new
// issue is never claimed, and switching the project to watch starts discovery
// without touching the tracked job.
func TestManualProjectTracksQueuedJobsInEveryDeliveryMode(t *testing.T) {
	for _, mode := range []string{"poll", "webhook", "browser"} {
		t.Run(mode, func(t *testing.T) {
			run := startManualTrackingRun(t, mode)
			if err := run.w.RequestFix("o/r", "", 7); err != nil {
				t.Fatal(err)
			}
			run.awaitDispatch(t, 7, "a hand-queued fix")
			run.awaitStatus(t, 7, "paused")

			// The run left its pull request open, so the job waits on it,
			// still tracked, and the matching #99 is never claimed, not even
			// when a webhook delivers it being opened.
			if mode == "webhook" {
				run.events <- WebhookEvent{Kind: "issues", Action: "opened", Repository: "o/r", IssueNumber: 99}
			}
			run.settle(t)
			run.expectNoDispatch(t, "a manual project with a job waiting on its pull request")
			if requests := run.w.fixRequests().forTarget("o/r"); len(requests) != 1 || requests[0].Number != 7 {
				t.Fatalf("a job whose issue is open stopped being tracked: requests = %+v", requests)
			}

			// An allowed commenter's mention wakes the waiting job.
			body := "Please revisit this @/aviary:SELF"
			run.comments.inject("o/r", 7, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now()})
			if mode == "webhook" {
				run.events <- WebhookEvent{Kind: "issue_comment", Action: "created", Repository: "o/r", IssueNumber: 7, CommentBody: body, CommentAuthor: "lsegal"}
			} else {
				run.mentions.add("o/r", RecentComment{ID: 42, Number: 7, Body: body, Author: "lsegal", UpdatedAt: time.Now()})
			}
			run.awaitDispatch(t, 7, "an allowed commenter's mention")
			run.awaitStatus(t, 7, "paused")

			// Its pull request closing without a merge restarts the work.
			run.src.fakeClosureSource.mu.Lock()
			run.src.fakeClosureSource.state = OriginatingWorkState{IssueState: "open", PullRequests: []PullRequestWorkState{{Number: 8, State: "closed"}}}
			run.src.fakeClosureSource.mu.Unlock()
			run.awaitDispatch(t, 7, "a rejected pull request")

			// The fresh attempt's pull request merges and closes the issue,
			// which ends the tracking.
			run.src.fakeClosureSource.mu.Lock()
			run.src.fakeClosureSource.state = OriginatingWorkState{IssueState: "closed", PullRequests: []PullRequestWorkState{{Number: 9, State: "merged", Merged: true}}}
			run.src.fakeClosureSource.mu.Unlock()
			run.src.setState("closed")
			if mode == "webhook" {
				run.events <- WebhookEvent{Kind: "issues", Action: "closed", Repository: "o/r", IssueNumber: 7}
			}
			run.awaitStatus(t, 7, "completed")
			waitFor(t, "the merged issue to stop being tracked", func() bool { return len(run.w.fixRequests().forTarget("o/r")) == 0 })
			if calls := run.src.listCalls(); calls != 0 {
				t.Fatalf("a manual project searched for issues %d time(s)", calls)
			}
			run.expectNoDispatch(t, "a manual project")

			// Switching to watch starts discovery, and leaves #7 alone.
			run.w.Manual.Set(nil)
			run.w.nudgePoll()
			if mode == "webhook" {
				run.events <- WebhookEvent{Kind: "issues", Action: "opened", Repository: "o/r", IssueNumber: 99}
			}
			run.awaitDispatch(t, 99, "switching to watch")
		})
	}
}

// Work already in flight when a project switches to manual keeps being
// tracked: its unfinished issue jobs are adopted as tracked issues.
func TestManualProjectAdoptsUnfinishedWork(t *testing.T) {
	run := startManualTrackingRun(t, "poll")
	waitFor(t, "a poll", func() bool {
		snapshot := run.w.published.Load()
		return snapshot != nil && !snapshot.LastPoll.IsZero()
	})
	run.w.adoptTrackedWork("o/r", []string{"o/r#7", "other/repo#3", "o/r#discussion#2"})
	if requests := run.w.fixRequests().forTarget("o/r"); len(requests) != 1 || requests[0].Number != 7 || requests[0].Repository != "o/r" {
		t.Fatalf("requests = %+v, want only #7 adopted", requests)
	}
}
