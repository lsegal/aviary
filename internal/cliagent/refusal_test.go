package cliagent

import (
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/transcript"
)

// TestToolTranslator_NamesTheCLIRefusalLayer checks a tool error that is the
// CLI's own permission refusal gets the note naming that layer, and other
// errors do not (#560).
func TestToolTranslator_NamesTheCLIRefusalLayer(t *testing.T) {
	var events []Event
	tr := newToolTranslator(func(e Event) { events = append(events, e) }, func(string) bool { return false })
	tr.refusalNote = "Refused by the claude CLI's own permission check, not by Aviary."
	refusal := `Claude requested permissions to write to C:\a\notes.md, but you haven't granted it yet.`
	tr.translate([]transcript.Event{
		{Kind: transcript.KindToolCall, CallID: "1", Name: "Write"},
		{Kind: transcript.KindToolResult, CallID: "1", Output: refusal, IsError: true},
		{Kind: transcript.KindToolCall, CallID: "2", Name: "Bash"},
		{Kind: transcript.KindToolResult, CallID: "2", Output: "exit status 1", IsError: true},
	})
	var errs []string
	for _, e := range events {
		if e.Type == EventToolResult {
			errs = append(errs, e.Tool.Error)
		}
	}
	if len(errs) != 2 {
		t.Fatalf("results = %q", errs)
	}
	if !strings.HasPrefix(errs[0], refusal) || !strings.HasSuffix(errs[0], tr.refusalNote) {
		t.Errorf("refusal error = %q, want the CLI's text then the note", errs[0])
	}
	if errs[1] != "exit status 1" {
		t.Errorf("ordinary error = %q, want it unchanged", errs[1])
	}
}

// TestClaudeArgs_PassesPermissions checks the translated permission rules
// reach claude's command line.
func TestClaudeArgs_PassesPermissions(t *testing.T) {
	args := claudeArgs(Options{Model: "opus", BuiltinTools: true, Permissions: []string{"--allowedTools", "Bash"}}, "", "")
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--allowedTools Bash") {
		t.Errorf("args = %q, want the permission rules", args)
	}
}
