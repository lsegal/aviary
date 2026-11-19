package projects

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/projects/agents"
)

// stubAgentVersion answers `binary --version` for the test instead of running
// one, so the check is exercised with no CLI installed.
func stubAgentVersion(t *testing.T, output string, err error) *[]string {
	t.Helper()
	var asked []string
	previous := agentVersionCommand
	agentVersionCommand = func(_ context.Context, binary string) ([]byte, error) {
		asked = append(asked, binary)
		return []byte(output), err
	}
	t.Cleanup(func() { agentVersionCommand = previous })
	return &asked
}

// TestCheckAgentVersion covers the paths a declared minimum has before a
// dispatch: an installed binary that is too old fails naming both versions, one
// at or above the minimum passes silently, and one whose version cannot be read
// -- either because the output carries none or because asking failed -- warns
// and proceeds.
func TestCheckAgentVersion(t *testing.T) {
	definition := agents.Definition{Name: "gemini", Binary: "gemini", MinVersion: "0.58.0"}
	for _, tc := range []struct {
		name        string
		definition  agents.Definition
		output      string
		err         error
		wantErr     []string
		wantWarning []string
		wantAsked   bool
	}{
		{
			name: "below the minimum", definition: definition, output: "0.2.2",
			wantErr: []string{"gemini", "0.58.0", "0.2.2"}, wantAsked: true,
		},
		{
			name: "at the minimum", definition: definition, output: "0.58.0", wantAsked: true,
		},
		{
			name: "above the minimum", definition: definition, output: "gemini 1.4.0\n", wantAsked: true,
		},
		{
			name: "unreadable version", definition: definition, output: "version unavailable",
			wantWarning: []string{"could not read a version", "0.58.0"}, wantAsked: true,
		},
		{
			name: "asking failed", definition: definition, output: "", err: errors.New("exec: not found"),
			wantWarning: []string{"could not read", "not found"}, wantAsked: true,
		},
		{
			name: "no minimum declared", definition: agents.Definition{Name: "codex", Binary: "codex"}, output: "0.0.1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			asked := stubAgentVersion(t, tc.output, tc.err)
			warning, err := checkAgentVersion(context.Background(), tc.definition, "/opt/bin/"+tc.definition.Binary)
			if len(*asked) > 0 != tc.wantAsked {
				t.Fatalf("version asked of %v, wantAsked %v", *asked, tc.wantAsked)
			}
			if len(tc.wantErr) == 0 && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %v, want it to mention %q", err, want)
				}
			}
			if len(tc.wantWarning) == 0 && warning != "" {
				t.Fatalf("unexpected warning: %q", warning)
			}
			for _, want := range tc.wantWarning {
				if !strings.Contains(warning, want) {
					t.Fatalf("warning = %q, want it to mention %q", warning, want)
				}
			}
		})
	}
}

// TestDispatchStopsBelowMinimumVersion proves the check runs before the agent
// does: a binary older than the definition requires never gets invoked, so the
// run reports the version rather than the CLI reporting an unrecognized
// argument.
func TestDispatchStopsBelowMinimumVersion(t *testing.T) {
	definition, record := fakeAgentRun{}.install(t, agents.Definition{
		Name: "gemini", Binary: "gemini", MinVersion: "0.58.0",
		Args: agents.Args{
			Run:    []agents.Fragment{{Args: []string{"--session-id", "{session}", "-p", "{prompt}"}}},
			Resume: []agents.Fragment{{Args: []string{"--resume", "{session}", "-p", "{prompt}"}}},
		},
		Session: agents.Session{Assign: agents.AssignEngine}, Output: agents.Output{Format: agents.FormatText},
	})
	registry, err := agents.NewRegistry(definition)
	if err != nil {
		t.Fatalf("register gemini: %v", err)
	}
	stubAgentVersion(t, "0.2.2", nil)
	runner := CommandRunner{Agent: "gemini", Definitions: registry, Repo: "o/r"}
	err = runner.RunSession(context.Background(), Issue{Number: 535, Target: "o/r"}, AgentSession{Agent: "gemini", ID: "session-1"}, func(AgentSession) {})
	if err == nil {
		t.Fatal("dispatch succeeded, want it to fail on the version")
	}
	for _, want := range []string{"gemini", "0.58.0", "0.2.2"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want it to mention %q", err, want)
		}
	}
	if _, err := os.Stat(record); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("agent recorded invocations (%v), want none", err)
	}
}

// TestDispatchProceedsAtMinimumVersion is the same dispatch with an install the
// definition accepts, which has to reach the agent unchanged.
func TestDispatchProceedsAtMinimumVersion(t *testing.T) {
	definition, record := fakeAgentRun{Stdout: "done"}.install(t, agents.Definition{
		Name: "gemini", Binary: "gemini", MinVersion: "0.58.0",
		Args: agents.Args{
			Run:    []agents.Fragment{{Args: []string{"--session-id", "{session}", "-p", "{prompt}"}}},
			Resume: []agents.Fragment{{Args: []string{"--resume", "{session}", "-p", "{prompt}"}}},
		},
		Session: agents.Session{Assign: agents.AssignEngine}, Output: agents.Output{Format: agents.FormatText},
	})
	registry, err := agents.NewRegistry(definition)
	if err != nil {
		t.Fatalf("register gemini: %v", err)
	}
	stubAgentVersion(t, "0.58.0", nil)
	runner := CommandRunner{Agent: "gemini", Definitions: registry, Repo: "o/r"}
	if err := runner.RunSession(context.Background(), Issue{Number: 535, Target: "o/r"}, AgentSession{Agent: "gemini", ID: "session-1"}, func(AgentSession) {}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if invocations := fakeAgentInvocationRecords(t, record); len(invocations) != 1 {
		t.Fatalf("agent was invoked %d times, want 1", len(invocations))
	}
}

// TestDispatchWarnsOnUnreadableVersion proves an install whose version cannot
// be read still runs, with the warning shown in the run's own output.
func TestDispatchWarnsOnUnreadableVersion(t *testing.T) {
	definition, record := fakeAgentRun{Stdout: "done"}.install(t, agents.Definition{
		Name: "gemini", Binary: "gemini", MinVersion: "0.58.0",
		Args: agents.Args{
			Run:    []agents.Fragment{{Args: []string{"-p", "{prompt}"}}},
			Resume: []agents.Fragment{{Args: []string{"-p", "{prompt}"}}},
		},
		Session: agents.Session{Assign: agents.AssignNone}, Output: agents.Output{Format: agents.FormatText},
	})
	registry, err := agents.NewRegistry(definition)
	if err != nil {
		t.Fatalf("register gemini: %v", err)
	}
	stubAgentVersion(t, "unknown build", nil)
	runner := CommandRunner{Agent: "gemini", Definitions: registry, Repo: "o/r"}
	var output strings.Builder
	if err := runner.RunSessionWithOutput(context.Background(), Issue{Number: 535, Target: "o/r"}, AgentSession{Agent: "gemini"}, func(AgentSession) {}, &output); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if !strings.Contains(output.String(), "0.58.0") {
		t.Fatalf("output = %q, want the version warning in it", output.String())
	}
	if invocations := fakeAgentInvocationRecords(t, record); len(invocations) != 1 {
		t.Fatalf("agent was invoked %d times, want 1", len(invocations))
	}
}
