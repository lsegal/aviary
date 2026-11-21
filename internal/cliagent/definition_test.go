package cliagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGeneric stands in for a CLI driven only through its definition. It
// answers in gemini's stream-json with the session it was started under, how,
// and the prompt it received. Started without a session it prints a captured
// one on stderr.
func fakeGeneric() {
	args := os.Args[1:]
	value := func(flag string) string {
		if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
			return args[i+1]
		}
		return ""
	}
	how, session := "new", value("--session-id")
	if id := value("--resume"); id != "" {
		how, session = "resume", id
	}
	if session == "gone" {
		fmt.Fprintln(os.Stderr, "Error: no previous sessions found")
		os.Exit(1)
	}
	if session == "" {
		session = "captured-1"
		fmt.Fprintln(os.Stderr, "session id: "+session)
	}
	out := json.NewEncoder(os.Stdout)
	reply := fmt.Sprintf("%s session=%s model=%s mode=%s prompt=%s", how, session, value("--model"), value("--mode"), value("-p"))
	_ = out.Encode(map[string]any{"type": "message", "role": "assistant", "content": reply[:4], "delta": true})
	_ = out.Encode(map[string]any{"type": "message", "role": "assistant", "content": reply[4:], "delta": true})
	_ = out.Encode(map[string]any{"type": "result", "stats": map[string]any{"input_tokens": 7, "output_tokens": 2}})
}

// useGenericDefinition installs a registry holding a definition for the fake
// CLI, assigning or capturing its session as assign says.
func useGenericDefinition(t *testing.T, assign string) Options {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	session := clidefs.Session{Assign: assign}
	if assign == clidefs.AssignCapture {
		session.Capture = `session id: (\S+)`
	}
	registry, err := clidefs.NewRegistry(clidefs.Definition{
		Name:     "fakecli",
		Binary:   exe,
		Session:  session,
		Output:   clidefs.Output{Format: clidefs.FormatGeminiJSON},
		Approval: map[clidefs.ApprovalMode][]string{clidefs.ApprovalReadOnly: {"--mode", "ro"}, clidefs.ApprovalAuto: {"--mode", "auto"}},
		Args: clidefs.Args{
			Run: []clidefs.Fragment{
				{When: "session", Args: []string{"--session-id", "{session}"}},
				{Args: []string{"{approval}"}},
				{When: "model", Args: []string{"--model", "{model}"}},
				{Args: []string{"-p", "{prompt}"}},
			},
			Resume: []clidefs.Fragment{
				{Args: []string{"--resume", "{session}"}},
				{Args: []string{"{approval}"}},
				{Args: []string{"-p", "{prompt}"}},
			},
		},
	})
	require.NoError(t, err)
	llm.SetCLIProviderRegistry(registry)
	t.Cleanup(func() { llm.SetCLIProviderRegistry(nil) })
	return Options{Backend: "fakecli", Model: "m1", WorkDir: t.TempDir(), Env: []string{"CLIAGENT_FAKE=generic"}}
}

func TestDefinitionSession_AssignedSessionResumes(t *testing.T) {
	opts := useGenericDefinition(t, clidefs.AssignEngine)
	b, model, err := ParseModel("fakecli/m1")
	require.NoError(t, err)
	assert.Equal(t, opts.Backend, b)
	assert.Equal(t, "m1", model)

	opts.NewSessionID = "sess-1"
	opts.SystemPrompt = "be brief"
	opts.BuiltinTools = true
	opts.Approval = []string{"--mode", "auto"}
	s, err := NewSession(opts)
	require.NoError(t, err)

	var events []Event
	r1, err := s.Send(context.Background(), Turn{Text: "hello"}, collect(&events))
	require.NoError(t, err)
	assert.Equal(t, "new session=sess-1 model=m1 mode=auto prompt=<system_instructions>\nbe brief\n</system_instructions>\n\nhello", r1.Text,
		"the first turn runs the run template under the assigned ID, opening with the system prompt")
	assert.Contains(t, events, Event{Type: EventSession, SessionID: "sess-1"})
	assert.Contains(t, events, Event{Type: EventUsage, Usage: &Usage{InputTokens: 7, OutputTokens: 2}})

	r2, err := s.Send(context.Background(), Turn{Text: "again"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "resume session=sess-1 model= mode=auto prompt=again", r2.Text, "a follow-up resumes the session with only the new message")

	// A new session for a stored ID resumes it from the first turn, as a chat
	// does after its process was dropped.
	opts.ResumeID = "sess-1"
	s, err = NewSession(opts)
	require.NoError(t, err)
	r3, err := s.Send(context.Background(), Turn{Text: "later"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "resume session=sess-1 model= mode=auto prompt=later", r3.Text)
}

func TestDefinitionSession_CapturedSessionResumes(t *testing.T) {
	opts := useGenericDefinition(t, clidefs.AssignCapture)
	s, err := NewSession(opts)
	require.NoError(t, err)

	var events []Event
	r1, err := s.Send(context.Background(), Turn{Text: "hello"}, collect(&events))
	require.NoError(t, err)
	assert.Equal(t, "new session=captured-1 model=m1 mode=ro prompt=hello", r1.Text, "without its own tools the CLI runs read-only")
	assert.Contains(t, events, Event{Type: EventSession, SessionID: "captured-1"})

	r2, err := s.Send(context.Background(), Turn{Text: "again"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "resume session=captured-1 model= mode=ro prompt=again", r2.Text)
}

func TestDefinitionSession_MissingSession(t *testing.T) {
	opts := useGenericDefinition(t, clidefs.AssignEngine)
	opts.ResumeID = "gone"
	s, err := NewSession(opts)
	require.NoError(t, err)
	_, err = s.Send(context.Background(), Turn{Text: "hi"}, nil)
	require.Error(t, err, "the runner starts a fresh session on a failed resume")
	assert.True(t, strings.Contains(err.Error(), "no previous sessions found"), err.Error())
}

func TestNewSession_RejectsDefinitionWithoutResume(t *testing.T) {
	_, err := NewSession(Options{Backend: "cline", Model: "m"})
	assert.ErrorContains(t, err, `unknown CLI backend "cline"`)
}
