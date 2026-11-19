package projects

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
)

// The create-issue tool runs gh-create through the prompt agent with this
// instance's identity, and reports the issue it filed.
func TestCreateIssueRunsGhCreateWithTheIdentity(t *testing.T) {
	var specs []agentSpec
	var prompts []string
	output := "Looked at https://github.com/o/r/issues/3 and https://github.com/x/y/issues/9.\nFiled https://github.com/o/r/issues/12\n"
	var runErr error
	w := &Engine{Identity: "ABCD1234", Registry: agents.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Out: &syncBuffer{}}
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
