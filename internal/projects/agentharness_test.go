package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/clidefs"
)

// The contract harness proves what an agent definition claims, without any
// vendor CLI being installed. The real ones cannot be run in CI -- they are
// absent from the runners and most need credentials -- so every definition is
// pointed at testdata/fakeagent instead, which records the argv, working
// directory, and environment it was invoked with and then behaves as the test
// told it to: announcing a session ID, printing an output stream, reporting a
// session it no longer holds, or failing outright.
//
// Definitions added later (issues #492 to #495) get their contract test by
// filling in an agentContract, so the harness has to be usable by definitions
// this file does not itself cover.

// Environment the fake agent reads. They are documented on the program itself.
const (
	fakeAgentRecordEnv   = "AVIARY_FAKE_AGENT_RECORD"
	fakeAgentStdoutEnv   = "AVIARY_FAKE_AGENT_STDOUT"
	fakeAgentSessionEnv  = "AVIARY_FAKE_AGENT_SESSION"
	fakeAgentCheckoutEnv = "AVIARY_FAKE_AGENT_CHECKOUT"
	fakeAgentWatchEnv    = "AVIARY_FAKE_AGENT_ENV"
	fakeAgentMissingEnv  = "AVIARY_FAKE_AGENT_MISSING"
	fakeAgentMissingText = "AVIARY_FAKE_AGENT_MISSING_TEXT"
	fakeAgentFailEnv     = "AVIARY_FAKE_AGENT_FAIL"
)

// fakeAgentCLI builds testdata/fakeagent once per test binary and returns the
// executable's path. It is built rather than checked in so it stays honest
// about the platform the tests run on: the same source produces the Windows
// and Unix stand-ins CI both exercise.
var fakeAgentCLI = sync.OnceValues(buildFakeAgentCLI)

// fakeAgentCLIDir holds the build output until the test binary exits.
var fakeAgentCLIDir string

func buildFakeAgentCLI() (string, error) {
	dir, err := os.MkdirTemp("", "aviary-fakeagent-")
	if err != nil {
		return "", err
	}
	fakeAgentCLIDir = dir
	binary := filepath.Join(dir, "fakeagent")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "./testdata/fakeagent")
	output, err := build.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build fake agent: %w: %s", err, output)
	}
	return binary, nil
}

// removeFakeAgentCLI drops the build output. TestMain calls it once the suite
// has finished.
func removeFakeAgentCLI() {
	if fakeAgentCLIDir != "" {
		_ = os.RemoveAll(fakeAgentCLIDir)
	}
}

// fakeAgentInvocation is one record the fake agent wrote.
type fakeAgentInvocation struct {
	Args []string          `json:"args"`
	Dir  string            `json:"dir"`
	Env  map[string]string `json:"env"`
}

// fakeAgentRun configures one invocation of the fake and reads back what it
// recorded, so a definition can be exercised without a CommandRunner at all.
type fakeAgentRun struct {
	// Stdout is written verbatim by the fake, with \n understood.
	Stdout string
	// Session is announced as the agent's own session ID.
	Session string
	// Checkout is announced as a AVIARY_CHECKOUT_DIRECTORY marker.
	Checkout string
	// WatchEnv names the environment variables the fake records.
	WatchEnv []string
	// MissingOn and FailOn name the 0-based invocation that reports a missing
	// session or exits non-zero. A nil value never does.
	MissingOn, FailOn *int
	// MissingText overrides what a missing session is reported as, for a
	// definition naming its own phrase.
	MissingText string
}

// install points a definition at the fake agent and configures its behaviour
// for the test, returning the definition to register and the record file the
// invocations land in.
func (f fakeAgentRun) install(t *testing.T, definition clidefs.Definition) (clidefs.Definition, string) {
	t.Helper()
	binary, err := fakeAgentCLI()
	if err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(t.TempDir(), "invocations.jsonl")
	t.Setenv(fakeAgentRecordEnv, record)
	t.Setenv(fakeAgentStdoutEnv, f.Stdout)
	t.Setenv(fakeAgentSessionEnv, f.Session)
	t.Setenv(fakeAgentCheckoutEnv, f.Checkout)
	t.Setenv(fakeAgentWatchEnv, strings.Join(f.WatchEnv, ","))
	t.Setenv(fakeAgentMissingEnv, invocationValue(f.MissingOn))
	t.Setenv(fakeAgentMissingText, f.MissingText)
	t.Setenv(fakeAgentFailEnv, invocationValue(f.FailOn))
	// A definition declaring a minimum CLI version has that version asked of
	// its binary before every dispatch. The fake agent has no
	// version of its own, so the check is answered here with exactly the
	// minimum: the harness is proving argv, not the version gate, which has
	// its own tests.
	if definition.MinVersion != "" {
		stubAgentVersion(t, definition.MinVersion, nil)
	}
	definition.Binary = binary
	return definition, record
}

func invocationValue(index *int) string {
	if index == nil {
		return ""
	}
	return strconv.Itoa(*index)
}

func invocation(index int) *int { return &index }

// fakeAgentInvocations reads every invocation recorded so far.
func fakeAgentInvocationRecords(t *testing.T, record string) []fakeAgentInvocation {
	t.Helper()
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read fake agent record: %v", err)
	}
	var invocations []fakeAgentInvocation
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry fakeAgentInvocation
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode fake agent record %q: %v", line, err)
		}
		invocations = append(invocations, entry)
	}
	return invocations
}

// agentContract is what one definition claims about the CLI it describes. The
// harness runs a fresh dispatch, a resume, and a resume whose session is gone
// against the fake agent and checks every claim.
type agentContract struct {
	// Definition is the agent being proved. Its Binary is replaced by the fake.
	Definition clidefs.Definition
	// Repo and Number name the issue the dispatch is for.
	Repo   string
	Number int
	// SessionID is the session the run carries: assigned by aviary, or
	// announced by the agent, depending on the definition.
	SessionID string
	// Approval and RemoteControl mirror the run's own settings.
	Approval      clidefs.ApprovalMode
	RemoteControl bool
	// Stdout is what the agent prints on a successful run.
	Stdout string
	// WantRun and WantResume are the exact argv each mode has to render.
	WantRun, WantResume []string
	// WantEnv is the environment the child process has to receive.
	WantEnv map[string]string
	// WantOutput is what the decoded output has to contain, which for an agent
	// whose output is a JSON stream is not what it printed.
	WantOutput string
	// MissingText is what the agent prints when it no longer holds the session
	// being resumed, empty for the shared default wording.
	MissingText string
}

// freshPrompt and resumePrompt are the prompts aviary sends, exposed so a
// contract can state the argv it expects in full.
func freshPrompt(repo string, number int) string {
	return fmt.Sprintf("/gh-fix %s#%d", repo, number) + "\n\nKeep your responses concise. Do not include code diffs or large code blocks; summarize the changes and tests instead."
}

func resumePrompt() string {
	return "continue\n\nRecover the existing work. If this issue has a draft pull request, inspect it and pull its branch before continuing."
}

// check runs the whole contract.
func (c agentContract) check(t *testing.T) {
	t.Helper()
	t.Run("fresh", c.checkFresh)
	t.Run("resume", c.checkResume)
	t.Run("resume restart", c.checkResumeRestart)
}

// runner registers the definition, pointed at the fake, and builds a runner
// that dispatches through it.
func (c agentContract) runner(t *testing.T, behaviour fakeAgentRun) (CommandRunner, string) {
	t.Helper()
	watched := make([]string, 0, len(c.WantEnv))
	for name := range c.WantEnv {
		watched = append(watched, name)
	}
	behaviour.WatchEnv = append(behaviour.WatchEnv, watched...)
	definition, record := behaviour.install(t, c.Definition)
	registry, err := clidefs.NewRegistry(definition)
	if err != nil {
		t.Fatalf("register %q: %v", definition.Name, err)
	}
	return CommandRunner{
		Agent: definition.Name, Definitions: registry, Repo: c.Repo,
		Approval: c.Approval, RemoteControl: c.RemoteControl,
	}, record
}

func (c agentContract) issue() Issue { return Issue{Number: c.Number, Target: c.Repo} }

// checkFresh dispatches a new issue and checks the argv, the environment, the
// session ID aviary ends up holding, and the decoded output.
func (c agentContract) checkFresh(t *testing.T) {
	behaviour := fakeAgentRun{Stdout: c.Stdout}
	session := AgentSession{Agent: c.Definition.Name}
	if c.Definition.AssignsSessionID() {
		session.ID = c.SessionID
	} else {
		behaviour.Session = c.SessionID
	}
	runner, record := c.runner(t, behaviour)
	var updates []AgentSession
	var output strings.Builder
	err := runner.RunSessionWithOutput(context.Background(), c.issue(), session, func(update AgentSession) {
		updates = append(updates, update)
	}, &output)
	if err != nil {
		t.Fatalf("fresh dispatch: %v", err)
	}
	invocations := fakeAgentInvocationRecords(t, record)
	if len(invocations) != 1 {
		t.Fatalf("fresh dispatch made %d invocations, want 1", len(invocations))
	}
	if got := invocations[0].Args; !reflect.DeepEqual(got, c.WantRun) {
		t.Fatalf("fresh argv = %#v, want %#v", got, c.WantRun)
	}
	for name, want := range c.WantEnv {
		if got := invocations[0].Env[name]; got != want {
			t.Fatalf("child environment %s = %q, want %q", name, got, want)
		}
	}
	if captured := capturedSessionID(updates); c.Definition.CapturesSessionID() && captured != c.SessionID {
		t.Fatalf("captured session ID = %q, want the one the agent announced (%q)", captured, c.SessionID)
	} else if c.Definition.AssignsSessionID() && captured != "" {
		t.Fatalf("captured session ID = %q, want none for an agent aviary assigns IDs to", captured)
	}
	if c.WantOutput != "" && !strings.Contains(output.String(), c.WantOutput) {
		t.Fatalf("decoded output = %q, want it to contain %q", output.String(), c.WantOutput)
	}
}

func capturedSessionID(updates []AgentSession) string {
	for _, update := range updates {
		if update.ID != "" {
			return update.ID
		}
	}
	return ""
}

// checkResume continues an existing session and checks the resume argv.
func (c agentContract) checkResume(t *testing.T) {
	runner, record := c.runner(t, fakeAgentRun{Stdout: c.Stdout})
	session := AgentSession{ID: c.SessionID, Agent: c.Definition.Name, Resume: true}
	if err := runner.RunSession(context.Background(), c.issue(), session, func(AgentSession) {}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	invocations := fakeAgentInvocationRecords(t, record)
	if len(invocations) != 1 {
		t.Fatalf("resume made %d invocations, want 1", len(invocations))
	}
	if got := invocations[0].Args; !reflect.DeepEqual(got, c.WantResume) {
		t.Fatalf("resume argv = %#v, want %#v", got, c.WantResume)
	}
}

// checkResumeRestart resumes a session the agent no longer holds. The work has
// to start over rather than being reported as a failure, and an agent that
// assigns its own session IDs has to be started without the dead one.
func (c agentContract) checkResumeRestart(t *testing.T) {
	runner, record := c.runner(t, fakeAgentRun{Stdout: c.Stdout, MissingOn: invocation(0), MissingText: c.MissingText})
	session := AgentSession{ID: c.SessionID, Agent: c.Definition.Name, Resume: true}
	if err := runner.RunSession(context.Background(), c.issue(), session, func(AgentSession) {}); err != nil {
		t.Fatalf("a resumed session the agent no longer holds should restart the work, got %v", err)
	}
	invocations := fakeAgentInvocationRecords(t, record)
	if len(invocations) != 2 {
		t.Fatalf("restart made %d invocations, want a resume followed by a fresh run", len(invocations))
	}
	if got := invocations[0].Args; !reflect.DeepEqual(got, c.WantResume) {
		t.Fatalf("first invocation = %#v, want the resume %#v", got, c.WantResume)
	}
	restarted := invocations[1].Args
	if reflect.DeepEqual(restarted, c.WantResume) {
		t.Fatalf("second invocation resumed again: %#v", restarted)
	}
	if !strings.Contains(strings.Join(restarted, " "), freshPrompt(c.Repo, c.Number)) {
		t.Fatalf("second invocation = %#v, want a fresh dispatch of the issue", restarted)
	}
	carried := strings.Contains(strings.Join(restarted, " "), c.SessionID)
	if c.Definition.Session.ClearOnResumeFailure && carried {
		t.Fatalf("second invocation = %#v, want the dead session ID dropped", restarted)
	}
	if !c.Definition.Session.ClearOnResumeFailure && !carried {
		t.Fatalf("second invocation = %#v, want the session ID aviary assigned kept", restarted)
	}
}

// builtinDefinition resolves one of the definitions aviary ships.
func builtinDefinition(t *testing.T, name string) clidefs.Definition {
	t.Helper()
	definition, ok := clidefs.MustBuiltin().Lookup(name)
	if !ok {
		t.Fatalf("no built-in definition for %q", name)
	}
	return definition
}

// TestCodexDefinitionContract proves the shipped codex definition against the
// fake CLI: the argv of a fresh run and a resume, the session ID it captures
// from the agent's own output, and the restart when a resume finds no session.
func TestCodexDefinitionContract(t *testing.T) {
	agentContract{
		Definition: builtinDefinition(t, "codex"),
		Repo:       "o/r",
		Number:     7,
		SessionID:  "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
		Stdout:     "working on it",
		// The spec names no model, so the argv carries the definition's
		// own defaultModel rather than leaving the choice to the CLI. The
		// session has no checkout yet, so the run starts in the temp
		// directory and skips Codex's git repo check.
		WantRun:    []string{"exec", "--json", "--skip-git-repo-check", "-c", "sandbox_mode=workspace-write", "-c", "sandbox_workspace_write.network_access=true", "--model", "gpt-6.1-terra", freshPrompt("o/r", 7)},
		WantResume: []string{"exec", "resume", "--json", "--skip-git-repo-check", "-c", "sandbox_mode=workspace-write", "-c", "sandbox_workspace_write.network_access=true", "--model", "gpt-6.1-terra", "3f2504e0-4f89-11d3-9a0c-0305e82c3301", resumePrompt()},
		WantOutput: "working on it",
	}.check(t)
}

// TestClaudeDefinitionContract proves the shipped claude definition: aviary
// assigns the session ID, the child gets the environment the definition names,
// and the JSON event stream is decoded rather than shown raw.
func TestClaudeDefinitionContract(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	agentContract{
		Definition: builtinDefinition(t, "claude"),
		Repo:       "o/r",
		Number:     7,
		SessionID:  "session-7",
		Stdout:     `{"type":"assistant","message":{"content":[{"type":"text","text":"working on it"}]}}`,
		WantRun: []string{
			"-p", "--session-id", "session-7", "--permission-mode", "auto",
			"--model", "sonnet",
			"--output-format", "stream-json", "--verbose", freshPrompt("o/r", 7),
		},
		WantResume: []string{
			"-p", "--resume", "session-7", "--permission-mode", "auto",
			"--output-format", "stream-json", "--verbose", resumePrompt(),
		},
		WantEnv: map[string]string{
			"CLAUDE_CODE_PRINT_BG_WAIT_CEILING_MS": "0",
			// Stored sign-in stays keyed to the user's own directory.
			"CLAUDE_SECURESTORAGE_CONFIG_DIR": home,
		},
		WantOutput: "working on it",
	}.check(t)
}

// TestClaudeJobTrustsItsFreshClone shows a Claude job resumed in its fresh,
// never-trusted clone runs against aviary's CLAUDE_CONFIG_DIR overlay, whose
// .claude.json copy records the clone as trusted. Claude otherwise drops the
// permission allow entries of the clone's .claude settings, whatever the
// approval mode, and says so in the job's output. The user's own
// .claude.json is left byte-identical (#565).
func TestClaudeJobTrustsItsFreshClone(t *testing.T) {
	home, userFile := t.TempDir(), []byte(`{"numStartups": 3, "projects": {"/elsewhere": {"hasTrustDialogAccepted": true}}}`)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".claude.json"), userFile, 0o600))
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	clone := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(clone, ".git"), 0o700))

	definition, record := fakeAgentRun{WatchEnv: []string{"CLAUDE_CONFIG_DIR"}}.install(t, builtinDefinition(t, "claude"))
	registry, err := clidefs.NewRegistry(definition)
	require.NoError(t, err)
	runner := CommandRunner{Agent: definition.Name, Definitions: registry, Repo: "o/r", Approval: clidefs.ApprovalFull}
	session := AgentSession{ID: "session-7", Agent: definition.Name, Resume: true, CheckoutDirectory: clone}
	require.NoError(t, runner.RunSession(context.Background(), Issue{Number: 7, Target: "o/r"}, session, func(AgentSession) {}))

	invocations := fakeAgentInvocationRecords(t, record)
	require.Len(t, invocations, 1)
	assert.Contains(t, invocations[0].Args, "--dangerously-skip-permissions")
	overlay := invocations[0].Env["CLAUDE_CONFIG_DIR"]
	require.NotEmpty(t, overlay)
	assert.NotEqual(t, home, overlay)
	var copied struct {
		NumStartups int                        `json:"numStartups"`
		Projects    map[string]map[string]bool `json:"projects"`
	}
	data, err := os.ReadFile(filepath.Join(overlay, ".claude.json"))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &copied))
	assert.Equal(t, 3, copied.NumStartups, "the copy keeps the user's settings")
	assert.True(t, copied.Projects["/elsewhere"]["hasTrustDialogAccepted"])
	assert.True(t, copied.Projects[filepath.ToSlash(clone)]["hasTrustDialogAccepted"], "projects = %v", copied.Projects)

	got, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	require.NoError(t, err)
	assert.Equal(t, userFile, got, "the user's .claude.json changed")
}

// TestOpencodeDefinitionContract proves the shipped opencode definition. The
// CLI has no session ID aviary can either assign or read back -- `opencode run`
// prints none in its default output format, and `--session` only accepts an ID
// opencode itself minted -- so the definition declares no session at all and a
// resume is rendered as a plain `opencode run` carrying the recovery prompt.
// The gh-fix workflow is re-entrant and adopts the draft pull request already
// open, so restarting is the intended behaviour rather than a lost job.
//
// The stdout below is a captured `opencode run --format json` stream. Every
// event is one part of the running session, so the type is the top-level type
// and the text and the tool call both hang off part: a finished tool call
// carries its own name and the input it was called with, which is what lets
// the progress line read the same as Claude's.
func TestOpencodeDefinitionContract(t *testing.T) {
	agentContract{
		Definition: builtinDefinition(t, "opencode"),
		Repo:       "o/r",
		Number:     7,
		Stdout: `{"type":"step_start","timestamp":1788483088781,"sessionID":"ses_1","part":{"id":"prt_1","messageID":"msg_1","type":"step-start"}}\n` +
			`{"type":"text","timestamp":1788483088959,"sessionID":"ses_1","part":{"id":"prt_2","messageID":"msg_1","type":"text","text":"reading the file"}}\n` +
			`{"type":"tool_use","timestamp":1788483102248,"sessionID":"ses_1","part":{"type":"tool","tool":"read","callID":"call_1","state":{"status":"completed","input":{"filePath":"/tmp/a.txt"},"output":"1: hi"}}}\n` +
			`{"type":"step_finish","timestamp":1788483089046,"sessionID":"ses_1","part":{"id":"prt_3","reason":"stop","type":"step-finish","tokens":{"total":6238}}}`,
		// --auto is unconditional rather than gated on the run's --yolo:
		// `opencode run` cannot prompt, so anything it would have asked about
		// -- reaching the isolated clone outside the working directory, above
		// all -- is auto-rejected without it, and the job dies on a permission
		// nobody can grant. This is the same reason claude gets
		// --permission-mode auto when the run is not in yolo mode.
		WantRun:    []string{"run", "--auto", "--format", "json", freshPrompt("o/r", 7)},
		WantResume: []string{"run", "--auto", "--format", "json", resumePrompt()},
		WantOutput: "reading the file\nRunning: read /tmp/a.txt",
	}.check(t)
}

// TestOpencodeDefinitionRendersModelLevelAndVision pins the rest of the
// opencode argv: the model is a provider/model pair, the reasoning level is a
// model variant, and a vision call hands the screenshot over with --file,
// which opencode reads through its own read tool and attaches as an image.
func TestOpencodeDefinitionRendersModelLevelAndVision(t *testing.T) {
	definition := builtinDefinition(t, "opencode")
	prompt := "/gh-fix o/r#7"
	for _, test := range []struct {
		name   string
		mode   clidefs.Mode
		values clidefs.Values
		want   []string
	}{
		{
			name: "run with model and level", mode: clidefs.ModeRun,
			values: clidefs.Values{Prompt: prompt, Model: "anthropic/claude-opus-5", Level: "high"},
			want:   []string{"run", "--auto", "--model", "anthropic/claude-opus-5", "--variant", "high", "--format", "json", prompt},
		},
		{
			// The run's own --yolo adds nothing: --auto is already the only
			// permission mode a non-interactive opencode can work in.
			name: "run in yolo mode", mode: clidefs.ModeRun,
			values: clidefs.Values{Prompt: prompt, Approval: clidefs.ApprovalFull},
			want:   []string{"run", "--auto", "--format", "json", prompt},
		},
		{
			// opencode reads no remote-control settings, so the run's flag
			// reaches its argv not at all.
			name: "run ignores remote control", mode: clidefs.ModeRun,
			values: clidefs.Values{Prompt: prompt, RemoteControl: true, Settings: `{"remoteControlAtStartup":true}`, SessionName: "aviary o/r#7"},
			want:   []string{"run", "--auto", "--format", "json", prompt},
		},
		{
			// A session ID aviary happens to be holding is never rendered: the
			// definition assigns none, so there is nothing to resume by.
			name: "resume carries no session", mode: clidefs.ModeResume,
			values: clidefs.Values{Prompt: prompt, Session: "ses_1a2b3c"},
			want:   []string{"run", "--auto", "--format", "json", prompt},
		},
		{
			// The one-shot board read stays on opencode's default format:
			// its answer is parsed as prose rather than shown as progress.
			name: "vision", mode: clidefs.ModeVision,
			values: clidefs.Values{Prompt: prompt, Image: "/tmp/shot.png", Model: "anthropic/claude-opus-5"},
			want:   []string{"run", "--auto", "--file", "/tmp/shot.png", "--model", "anthropic/claude-opus-5", prompt},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := definition.Render(test.mode, test.values); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("argv = %#v, want %#v", got, test.want)
			}
		})
	}
	if definition.AssignsSessionID() || definition.CapturesSessionID() {
		t.Fatal("opencode declares a session ID aviary can neither assign nor capture")
	}
	if !definition.AcceptsLevel("high") || definition.AcceptsLevel("ultra") {
		t.Fatal("opencode levels are not validated against the definition")
	}
}

// TestGeminiDefinitionContract proves the shipped gemini definition. Gemini
// CLI takes a caller-supplied UUID with --session-id and resumes it by that
// same UUID with --resume, so aviary assigns the ID exactly as it does for
// Claude. Its headless mode refuses to run in a directory the user has not
// trusted interactively, which every aviary run is -- the work happens in a
// fresh clone -- so the definition carries GEMINI_CLI_TRUST_WORKSPACE rather
// than passing --skip-trust, and the child process is checked for it here.
// Its --output-format stream-json emits its own event shape rather than
// Claude's, decoded by the gemini-json format (#388): the stdout below is
// captured from gemini-cli 0.58.0, whose assistant text arrives as deltas the
// decoder joins back into one line.
func TestGeminiDefinitionContract(t *testing.T) {
	agentContract{
		Definition: builtinDefinition(t, "gemini"),
		Repo:       "o/r",
		Number:     7,
		SessionID:  "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
		Stdout: `{"type":"init","timestamp":"2026-11-19T11:13:09.402Z","session_id":"3f2504e0-4f89-11d3-9a0c-0305e82c3301","model":"gemini-3.5-flash"}\n` +
			`{"type":"message","timestamp":"2026-11-19T11:13:09.402Z","role":"user","content":"the prompt"}\n` +
			`{"type":"message","timestamp":"2026-11-19T11:13:09.531Z","role":"assistant","content":"working ","delta":true}\n` +
			`{"type":"message","timestamp":"2026-11-19T11:13:09.531Z","role":"assistant","content":"on it","delta":true}\n` +
			`{"type":"tool_use","timestamp":"2026-11-19T11:13:09.462Z","tool_name":"run_shell_command","tool_id":"run_shell_command_1","parameters":{"command":"echo hi"}}\n` +
			`{"type":"tool_result","timestamp":"2026-11-19T11:13:09.483Z","tool_id":"run_shell_command_1","status":"success","output":"hi"}\n` +
			`{"type":"result","timestamp":"2026-11-19T11:13:09.532Z","status":"success","stats":{"input_tokens":10,"output_tokens":2,"cached":0,"input":10}}`,
		// The spec names no model, so the argv carries the definition's own
		// defaultModel.
		WantRun: []string{
			"--session-id", "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
			"--approval-mode", "auto_edit", "--model", "gemini-3.5-flash",
			"--output-format", "stream-json",
			"-p", freshPrompt("o/r", 7),
		},
		WantResume: []string{
			"--resume", "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
			"--approval-mode", "auto_edit", "--output-format", "stream-json",
			"-p", resumePrompt(),
		},
		WantEnv:    map[string]string{"GEMINI_CLI_TRUST_WORKSPACE": "true"},
		WantOutput: "working on it\nRunning: echo hi",
	}.check(t)
}

// TestGeminiDefinitionYoloContract pins the other half of the approval
// switch: --yolo replaces --approval-mode auto_edit rather than being added
// alongside it, which Gemini CLI would reject as contradictory.
func TestGeminiDefinitionYoloContract(t *testing.T) {
	agentContract{
		Definition: builtinDefinition(t, "gemini"),
		Repo:       "o/r",
		Number:     7,
		SessionID:  "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
		Approval:   clidefs.ApprovalFull,
		Stdout:     "working on it",
		WantRun: []string{
			"--session-id", "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
			"--yolo", "--model", "gemini-3.5-flash",
			"--output-format", "stream-json", "-p", freshPrompt("o/r", 7),
		},
		WantResume: []string{
			"--resume", "3f2504e0-4f89-11d3-9a0c-0305e82c3301",
			"--yolo", "--output-format", "stream-json", "-p", resumePrompt(),
		},
		WantEnv: map[string]string{"GEMINI_CLI_TRUST_WORKSPACE": "true"},
	}.check(t)
}

// TestGeminiVisionArgsNameTheImageInThePrompt pins the one-shot browser-board
// read. Gemini CLI has no --image flag, so the screenshot is handed over the
// way Claude's is, by the path the vision prompt already names, which its
// read_file tool loads as an image part.
func TestGeminiVisionArgsNameTheImageInThePrompt(t *testing.T) {
	definition := builtinDefinition(t, "gemini")
	args := definition.Render(clidefs.ModeVision, clidefs.Values{
		Prompt: "look at /tmp/shot.png", Image: "/tmp/shot.png", Model: "gemini-2.5-pro",
	})
	want := []string{"--approval-mode", "auto_edit", "--model", "gemini-2.5-pro", "-p", "look at /tmp/shot.png"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("vision argv = %#v, want %#v", args, want)
	}
	for _, arg := range args {
		if arg == "--image" {
			t.Fatal("gemini has no --image flag; the screenshot is named in the prompt")
		}
	}
}

// TestGeminiResumeFailureMessagesRestartTheWork pins the wordings Gemini CLI
// prints when the recorded session is gone -- one for a project with no
// session history and one for a project whose history does not hold that
// UUID -- against the messages aviary watches for. Without them a resume of an
// expired session is reported as an agent failure instead of restarting.
func TestGeminiResumeFailureMessagesRestartTheWork(t *testing.T) {
	for _, message := range []string{
		"Error resuming session: No previous sessions found for this project.",
		"Error resuming session: Invalid session identifier " +
			`"3f2504e0-4f89-11d3-9a0c-0305e82c3301".` +
			"\n  Searched for sessions in /tmp/chats.\n  Use --list-sessions to see available sessions,",
	} {
		// The wordings are gemini's own, so the detector is given the same
		// patterns a gemini run gives it: the shared defaults plus the ones
		// its definition names.
		detector := &missingSessionDetector{
			output:   io.Discard,
			patterns: builtinDefinition(t, "gemini").MissingSessionPatterns(),
		}
		if _, err := detector.Write([]byte(message)); err != nil {
			t.Fatal(err)
		}
		if !detector.sessionMissing() {
			t.Fatalf("aviary does not recognise %q as a session it can no longer resume", message)
		}
	}
}

// TestAgentDefinitionRunFailureIsReported checks a definition whose agent
// exits non-zero for a reason other than a missing session is reported as a
// failure rather than silently restarted.
func TestAgentDefinitionRunFailureIsReported(t *testing.T) {
	contract := agentContract{Definition: builtinDefinition(t, "codex"), Repo: "o/r", Number: 7}
	runner, record := contract.runner(t, fakeAgentRun{FailOn: invocation(0)})
	err := runner.RunSession(context.Background(), contract.issue(), AgentSession{Agent: "codex"}, func(AgentSession) {})
	if err == nil {
		t.Fatal("a failing agent run was reported as a success")
	}
	if got := len(fakeAgentInvocationRecords(t, record)); got != 1 {
		t.Fatalf("a plain failure made %d invocations, want 1", got)
	}
}

// TestAgentDefinitionCapturesCheckoutDirectory checks the marker an agent
// prints when it clones the repository is read back through the definition
// path, since a run that loses it dispatches later work in the wrong directory.
func TestAgentDefinitionCapturesCheckoutDirectory(t *testing.T) {
	checkout := t.TempDir()
	contract := agentContract{Definition: builtinDefinition(t, "codex"), Repo: "o/r", Number: 7}
	runner, _ := contract.runner(t, fakeAgentRun{Checkout: checkout})
	var updates []AgentSession
	if err := runner.RunSession(context.Background(), contract.issue(), AgentSession{Agent: "codex"}, func(update AgentSession) {
		updates = append(updates, update)
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	found := false
	for _, update := range updates {
		if update.CheckoutDirectory != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("updates = %#v, want the checkout directory the agent announced", updates)
	}
}

// TestUnknownAgentIsReportedRatherThanRun checks a work-state entry naming an
// agent the registry no longer defines fails with a message listing what it
// does define, instead of spawning whatever the binary flags happen to hold.
func TestUnknownAgentIsReportedRatherThanRun(t *testing.T) {
	runner := CommandRunner{Agent: "nosuchagent", Definitions: clidefs.MustBuiltin(), Repo: "o/r"}
	err := runner.Run(context.Background(), Issue{Number: 7, Target: "o/r"})
	if err == nil || !strings.Contains(err.Error(), `unknown agent "nosuchagent"`) {
		t.Fatalf("error = %v, want it to name the unknown agent", err)
	}
	for _, name := range clidefs.MustBuiltin().Names() {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("error = %v, want it to list the known agent %q", err, name)
		}
	}
}

// TestJSONLAgentDefinitionContract proves an agent whose output is neither
// plain text nor Claude's envelope: its event stream is decoded by the paths
// its own definition names, its session ID is read back with its own regular
// expression, and the phrase it prints for a session it no longer holds is its
// own rather than one of the shared defaults. Nothing about it exists in Go.
func TestJSONLAgentDefinitionContract(t *testing.T) {
	definition := clidefs.Definition{
		Name: "streamer", Binary: "streamer",
		Session: clidefs.Session{
			Assign: clidefs.AssignCapture, Capture: `session id: ([0-9a-z-]+)`,
			ClearOnResumeFailure: true,
		},
		MissingSession: []string{"thread has expired"},
		Output: clidefs.Output{Format: "jsonl", JSONL: &clidefs.JSONL{
			Type: "event", Text: "delta.text",
			ToolName: "delta.tool.name", ToolInput: "delta.tool.arguments",
			Ignore: []string{"usage"},
		}},
		Args: clidefs.Args{
			Run:    []clidefs.Fragment{{Args: []string{"start", "--json", "{prompt}"}}},
			Resume: []clidefs.Fragment{{Args: []string{"continue", "--json", "{session}", "{prompt}"}}},
		},
	}
	agentContract{
		Definition: definition,
		Repo:       "o/r",
		Number:     7,
		SessionID:  "9f31b0c2-thread",
		Stdout: `{"event":"usage","delta":{"text":"dropped"}}\n` +
			`{"event":"message","delta":{"text":"reading the issue"}}\n` +
			`{"event":"message","delta":{"tool":{"name":"Read","arguments":{"file_path":"main.go"}}}}`,
		WantRun:     []string{"start", "--json", freshPrompt("o/r", 7)},
		WantResume:  []string{"continue", "--json", "9f31b0c2-thread", resumePrompt()},
		WantOutput:  "reading the issue\nRunning: Read main.go",
		MissingText: "streamer: thread has expired, start a new one",
	}.check(t)
}

// TestJSONLAgentDoesNotRestartOnAnOrdinaryFailure proves the other half of the
// detector: output that names no missing-session phrase at all is reported as
// the agent failure it is rather than restarting the work.
func TestJSONLAgentDoesNotRestartOnAnOrdinaryFailure(t *testing.T) {
	definition := clidefs.Definition{
		Name: "streamer", Binary: "streamer",
		Session:        clidefs.Session{Assign: clidefs.AssignNone},
		MissingSession: []string{"thread has expired"},
		Output:         clidefs.Output{Format: "plain"},
		Args: clidefs.Args{
			Run:    []clidefs.Fragment{{Args: []string{"start", "{prompt}"}}},
			Resume: []clidefs.Fragment{{Args: []string{"continue", "{prompt}"}}},
		},
	}
	contract := agentContract{Definition: definition, Repo: "o/r", Number: 7}
	runner, record := contract.runner(t, fakeAgentRun{
		MissingOn: invocation(0), MissingText: "error: model overloaded, try again",
	})
	session := AgentSession{Agent: definition.Name, Resume: true}
	if err := runner.RunSession(context.Background(), contract.issue(), session, func(AgentSession) {}); err == nil {
		t.Fatal("an ordinary agent failure was treated as a missing session")
	}
	if got := fakeAgentInvocationRecords(t, record); len(got) != 1 {
		t.Fatalf("agent invocations = %d, want no restart", len(got))
	}
}

// TestConfiguredAgentDefinitionIsDispatchable proves the whole path an agent
// nobody built in takes: declared in cli_definitions, accepted as an agent spec,
// and dispatched through the executable its own definition names.
func TestConfiguredAgentDefinitionIsDispatchable(t *testing.T) {
	binary, err := fakeAgentCLI()
	if err != nil {
		t.Fatal(err)
	}
	document := fmt.Sprintf(`{"agents":[{
		"name": "acme",
		"binary": %q,
		"levels": ["fast", "thorough"],
		"session": {"assign": "capture", "capture": "conversation ([0-9a-z-]+)", "clearOnResumeFailure": true},
		"output": {"format": "text"},
		"args": {
			"run": [{"args": ["start"]}, {"when": "level", "args": ["--effort", "{level}"]}, {"args": ["{prompt}"]}],
			"resume": [{"args": ["continue", "{session}", "{prompt}"]}]
		}
	}]}`, binary)
	registry := loadDefinitions(t, document)
	spec, err := parseAgentSpecIn(registry, "acme:thorough")
	if err != nil {
		t.Fatalf("--agent acme:thorough was rejected: %v", err)
	}
	if want := (agentSpec{Name: "acme", Level: "thorough"}); spec != want {
		t.Fatalf("spec = %#v, want %#v", spec, want)
	}
	if _, err := parseAgentSpecIn(registry, "acme:high"); err == nil {
		t.Fatal("a level outside the definition's own list was accepted")
	}
	definition, ok := registry.Lookup("acme")
	if !ok {
		t.Fatal("the configured agent was not registered")
	}
	agentContract{
		Definition: definition,
		Repo:       "o/r",
		Number:     7,
		SessionID:  "abc-123",
		Stdout:     "conversation abc-123",
		WantRun:    []string{"start", freshPrompt("o/r", 7)},
		WantResume: []string{"continue", "abc-123", resumePrompt()},
		WantOutput: "conversation abc-123",
	}.check(t)
}

// loadDefinitions builds a registry from cli_definitions written as JSON:
// either an object keyed by agent name, or {"agents": [...]} with each
// definition naming itself.
func loadDefinitions(t *testing.T, document string) *clidefs.Registry {
	t.Helper()
	var top map[string]any
	if err := json.Unmarshal([]byte(document), &top); err != nil {
		t.Fatal(err)
	}
	defs := top
	if list, ok := top["agents"].([]any); ok {
		defs = map[string]any{}
		for _, raw := range list {
			definition := raw.(map[string]any)
			defs[definition["name"].(string)] = definition
		}
	}
	registry, err := clidefs.Load(defs)
	if err != nil {
		t.Fatalf("load definitions: %v", err)
	}
	return registry
}
