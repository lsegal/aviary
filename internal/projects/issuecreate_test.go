package projects

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/projects/core"
)

// The create-issue tool runs gh-create through the prompt agent with this
// instance's identity, and reports the issue it filed.
func TestCreateIssueRunsGhCreateWithTheIdentity(t *testing.T) {
	var specs []agentSpec
	var prompts []string
	output := "Looked at https://github.com/o/r/issues/3 and https://github.com/x/y/issues/9.\nFiled https://github.com/o/r/issues/12\n"
	var runErr error
	w := &Engine{Identity: "ABCD1234", Registry: clidefs.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Out: &syncBuffer{}}
	w.oneShotRun = func(_ context.Context, spec agentSpec, prompt string) (string, error) {
		specs = append(specs, spec)
		prompts = append(prompts, prompt)
		return output, runErr
	}
	result, err := w.CreateIssue(context.Background(), core.IssueCreate{Repository: "o/r", Text: " Add a dark mode "})
	if err != nil || result.URL != "https://github.com/o/r/issues/12" || result.Agent != "claude/opus" {
		t.Fatalf("CreateIssue = %+v, %v", result, err)
	}
	if want := "/gh-create o/r Add a dark mode identity:/aviary:ABCD1234"; prompts[0] != want {
		t.Errorf("prompt = %q, want %q", prompts[0], want)
	}
	if _, err := w.CreateIssue(context.Background(), core.IssueCreate{Repository: "o/r", Text: "x", Model: "sonnet"}); err != nil || specs[1].String() != "claude/sonnet" {
		t.Errorf("model override ran %s, %v; want claude/sonnet", specs[1], err)
	}
	output = "I could not file it: gh is signed out."
	if _, err := w.CreateIssue(context.Background(), core.IssueCreate{Repository: "o/r", Text: "x"}); err == nil || !strings.Contains(err.Error(), "signed out") {
		t.Errorf("output with no new issue returned %v", err)
	}
	runErr = errors.New("exit status 1")
	if _, err := w.CreateIssue(context.Background(), core.IssueCreate{Repository: "o/r", Text: "x"}); err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Errorf("a failed run returned %v", err)
	}
	calls := len(prompts)
	for _, bad := range []core.IssueCreate{{Repository: "not a repo", Text: "x"}, {Repository: "o/r", Text: "   "}, {Repository: "o/r", Text: strings.Repeat("x", maxIssueDescription+1)}} {
		if _, err := w.CreateIssue(context.Background(), bad); err == nil {
			t.Errorf("CreateIssue(%+v) was accepted", bad)
		}
	}
	if len(prompts) != calls {
		t.Errorf("invalid requests ran the agent %d times", len(prompts)-calls)
	}
}

// A gh-create run that outlasts its caller keeps filing, and asking again
// with the same request waits for that run's issue instead of filing a
// duplicate (issue #710).
func TestCreateIssueOutlastingItsCallerFilesOnce(t *testing.T) {
	var runs atomic.Int32
	release := make(chan struct{})
	w := &Engine{Registry: clidefs.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Out: &syncBuffer{}}
	w.oneShotRun = func(ctx context.Context, _ agentSpec, _ string) (string, error) {
		runs.Add(1)
		select {
		case <-release: // gh-create takes longer than its first caller waits
		case <-ctx.Done():
			return "", ctx.Err()
		}
		return "Filed https://github.com/o/r/issues/12\n", nil
	}
	request := core.IssueCreate{Repository: "o/r", Text: "Add a dark mode"}

	// The first caller gives up, the way the CLI's MCP client does.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := w.CreateIssue(ctx, request); err == nil || !strings.Contains(err.Error(), "still filing") || !strings.Contains(err.Error(), "call project_issue_create again") {
		t.Fatalf("a caller that stopped waiting got %v", err)
	}

	// Asking again joins the run still filing.
	type outcome struct {
		result core.IssueCreateResult
		err    error
	}
	again := make(chan outcome, 1)
	go func() {
		result, err := w.CreateIssue(context.Background(), core.IssueCreate{Repository: "O/R", Text: " Add a dark mode "})
		again <- outcome{result, err}
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)
	got := <-again
	if got.err != nil || got.result.URL != "https://github.com/o/r/issues/12" {
		t.Fatalf("asking again = %+v, %v", got.result, got.err)
	}

	// Once filed, the same request returns the issue without filing again.
	result, err := w.CreateIssue(context.Background(), request)
	if err != nil || result.URL != "https://github.com/o/r/issues/12" {
		t.Fatalf("asking after it filed = %+v, %v", result, err)
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("gh-create ran %d times, want 1", n)
	}

	// A different request files its own issue.
	if _, err := w.CreateIssue(context.Background(), core.IssueCreate{Repository: "o/r", Text: "Add a light mode"}); err != nil || runs.Load() != 2 {
		t.Errorf("a different request ran gh-create %d times in all, %v", runs.Load(), err)
	}
}

// A failed filing is not remembered, so asking again retries it.
func TestCreateIssueRetriesAFailedFiling(t *testing.T) {
	output := "I could not file it: gh is signed out."
	runs := 0
	w := &Engine{Registry: clidefs.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Out: &syncBuffer{}}
	w.oneShotRun = func(context.Context, agentSpec, string) (string, error) {
		runs++
		return output, nil
	}
	request := core.IssueCreate{Repository: "o/r", Text: "x"}
	if _, err := w.CreateIssue(context.Background(), request); err == nil {
		t.Fatal("a filing with no new issue succeeded")
	}
	output = "Filed https://github.com/o/r/issues/4"
	if result, err := w.CreateIssue(context.Background(), request); err != nil || result.URL != "https://github.com/o/r/issues/4" || runs != 2 {
		t.Errorf("retry = %+v, %v after %d runs", result, err, runs)
	}
}

func TestCreatedIssueURLMatchesWholeURLsOnly(t *testing.T) {
	for _, test := range []struct {
		output, want string
	}{
		{"Filed https://github.com/o/r/issues/12.", "https://github.com/o/r/issues/12"},
		{"see [#3](https://github.com/o/r/issues/3) then filed (https://github.com/O/R/issues/9)", "https://github.com/O/R/issues/9"},
		{"filed `https://github.com/o/r/issues/4`, done", "https://github.com/o/r/issues/4"},
		{"https://github.com/o/r/issues/5 and https://github.com/x/y/issues/6", "https://github.com/o/r/issues/5"},
		{"https://evil.example/https://github.com/o/r/issues/7", ""},
		{"https://github.com/o/r/issues/8.evil.example", ""},
		{"https://github.com/o/r/pull/9", ""},
	} {
		if got := createdIssueURL(test.output, "o/r"); got != test.want {
			t.Errorf("createdIssueURL(%q) = %q, want %q", test.output, got, test.want)
		}
	}
}
