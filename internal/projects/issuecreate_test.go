package projects

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
)

// The MCP ask tool waits for the prompt agent's answer, runs with the
// configured prompt-agent model unless the call names one, and shares the
// prompt area's conversation and scope (issue #807).
func TestAskPromptWaitsForTheAnswerAndHonorsAModelOverride(t *testing.T) {
	var specs []agentSpec
	var prompts []string
	w := &Glorp{Identity: "ABCD1234", Registry: agents.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Out: &syncBuffer{}}
	w.published.Store(&core.Snapshot{Jobs: []JobSnapshot{{Target: "o/r", Number: 7, Status: "paused", WaitReason: core.ManualPauseReason}}})
	w.promptRun = func(_ context.Context, spec agentSpec, prompt string) (string, error) {
		specs = append(specs, spec)
		prompts = append(prompts, prompt)
		return "o/r#7 is paused by owner.", nil
	}
	entry, err := w.AskPrompt(context.Background(), "why is 7 paused?", "")
	if err != nil || entry.Status != "done" || entry.Response != "o/r#7 is paused by owner." || entry.Agent != "claude/opus" {
		t.Fatalf("AskPrompt = %+v, %v", entry, err)
	}
	if !strings.Contains(prompts[0], "Refuse anything else") || !strings.Contains(prompts[0], "key: o/r#7") || !strings.Contains(prompts[0], "why is 7 paused?") {
		t.Errorf("ask prompt lacks the prompt agent's scope or state:\n%s", prompts[0])
	}
	if listed := w.Prompts(); len(listed) != 1 || listed[0].ID != entry.ID || listed[0].Status != "done" {
		t.Errorf("prompt area = %+v, want the asked request", listed)
	}
	if entry, err = w.AskPrompt(context.Background(), "again", "sonnet"); err != nil || specs[1].String() != "claude/sonnet" || entry.Agent != "claude/sonnet" {
		t.Errorf("model override ran %s (%+v, %v), want claude/sonnet", specs[1], entry, err)
	}
	w.SetPromptAgent(unblockConfig{Agent: "codex", Model: "gpt-5.5"})
	if _, err := w.AskPrompt(context.Background(), "and now?", ""); err != nil || specs[2].String() != "codex/gpt-5.5" {
		t.Errorf("after a prompt-agent change the ask ran %s, %v; want codex/gpt-5.5", specs[2], err)
	}
	if _, err := w.AskPrompt(context.Background(), "  ", ""); err == nil {
		t.Error("an empty request was accepted")
	}
}

// The create-issue tool runs gh-create through the prompt agent with this
// instance's identity, and reports the issue it filed (issue #807).
func TestCreateIssueRunsGhCreateWithTheIdentity(t *testing.T) {
	var specs []agentSpec
	var prompts []string
	output := "Looked at https://github.com/o/r/issues/3 and https://github.com/x/y/issues/9.\nFiled https://github.com/o/r/issues/12\n"
	var runErr error
	w := &Glorp{Identity: "ABCD1234", Registry: agents.MustBuiltin(), Runner: CommandRunner{Agent: "claude/opus", Agents: []string{"claude/opus"}}, Out: &syncBuffer{}}
	w.promptRun = func(_ context.Context, spec agentSpec, prompt string) (string, error) {
		specs = append(specs, spec)
		prompts = append(prompts, prompt)
		return output, runErr
	}
	result, err := w.CreateIssue(context.Background(), core.IssueCreate{Repository: "o/r", Text: " Add a dark mode "})
	if err != nil || result.URL != "https://github.com/o/r/issues/12" || result.Agent != "claude/opus" {
		t.Fatalf("CreateIssue = %+v, %v", result, err)
	}
	if want := "/gh-create o/r Add a dark mode identity:/glorp:ABCD1234"; prompts[0] != want {
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
