package projects

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/clidefs"
)

const oneShotHelperEnv = "AVIARY_ONESHOT_HELPER"

// oneShotHelperRecordEnv names a file the stand-in agent writes the prompt it
// received to, wherever it arrived from, when the test sets it.
const oneShotHelperRecordEnv = "AVIARY_ONESHOT_HELPER_RECORD"

// promptFilePattern reads the prompt file path back out of the pointer prompt
// an agent without stdin support is given.
var promptFilePattern = regexp.MustCompile(`in the file (.+?)\. Read the whole file`)

// TestOneShotPromptHelperProcess is the stand-in agent: it reports how many
// prompt arguments it got and how large a prompt reached it on stdin or
// through the prompt file, which is all a test needs to see that the agent
// started and was handed the whole prompt. When the test names a record file,
// the prompt itself is written there too.
func TestOneShotPromptHelperProcess(_ *testing.T) {
	if os.Getenv(oneShotHelperEnv) != "1" {
		return
	}
	args := os.Args[1:]
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) > 0 {
		args = args[1:]
	}
	stdin, _ := io.ReadAll(os.Stdin)
	file := 0
	received := string(stdin)
	if len(args) == 1 {
		received = args[0]
		if match := promptFilePattern.FindStringSubmatch(args[0]); match != nil {
			data, err := os.ReadFile(match[1])
			if err != nil {
				fmt.Printf("cannot read the prompt file: %v\n", err)
				os.Exit(1)
			}
			file = len(data)
			received = string(data)
		}
	}
	if record := os.Getenv(oneShotHelperRecordEnv); record != "" {
		if err := os.WriteFile(record, []byte(received), 0o600); err != nil {
			fmt.Printf("cannot record the prompt: %v\n", err)
			os.Exit(1)
		}
	}
	fmt.Printf("arguments=%d stdin=%d file=%d\n", len(args), len(stdin), file)
	os.Exit(0)
}

// Every out-of-band call is marked one-shot, so an agent definition can add
// the flags a call outside any checkout needs, such as Codex's
// --skip-git-repo-check.
func TestOneShotPromptMarksTheCallOneShot(t *testing.T) {
	codex, _ := clidefs.MustBuiltin().Lookup("codex")
	call, err := newOneShotPrompt(codex, clidefs.Values{Prompt: "status?"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer call.cleanup()
	want := []string{"exec", "--json", "--skip-git-repo-check", "-c", "sandbox_mode=workspace-write", "-c", "sandbox_workspace_write.network_access=true", "-"}
	if got := codex.Render(clidefs.ModeRun, call.Values); !reflect.DeepEqual(got, want) {
		t.Errorf("codex one-shot argv = %#v, want %#v", got, want)
	}
}

// A spawn that fails names the measured sizes and the limit they exceed,
// never the prompt itself.
func TestSpawnErrorNamesSizesAndLimit(t *testing.T) {
	cmd := newAgentCommand(context.Background(), filepath.Join(t.TempDir(), "missing-agent"), "-p", "secret prompt text")
	err := spawnError(cmd, errors.New("The filename or extension is too long.")) //nolint:revive // quotes the Windows error verbatim
	for _, want := range []string{"The filename or extension is too long.", "3 arguments", "command line about", "environment", "executable path"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("spawn error %q is missing %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "secret prompt text") {
		t.Errorf("spawn error leaks the prompt: %q", err)
	}
	for _, tc := range []struct {
		goos                string
		line, longest, path int
		want                string
	}{
		{"windows", 40000, 39000, 80, "Windows command-line limit of 32767"},
		{"windows", 1000, 900, 300, "MAX_PATH"},
		{"linux", 200000, 150000, 20, "Linux single-argument limit"},
		{"linux", 40000, 39000, 20, "no known size limit"},
	} {
		if got := spawnLimit(tc.goos, tc.line, tc.longest, tc.path); !strings.Contains(got, tc.want) {
			t.Errorf("spawnLimit(%s, %d, %d, %d) = %q, want %q", tc.goos, tc.line, tc.longest, tc.path, got, tc.want)
		}
	}
}

// oneShotRegistry registers two stand-in agents run through the test binary:
// one that reads its prompt from stdin and one that cannot.
func oneShotRegistry(t *testing.T) *clidefs.Registry {
	t.Helper()
	agent := func(name, stdin string) string {
		return fmt.Sprintf(`{"name":%q,"binary":%q,"session":{"assign":"none"},"output":{"format":"plain"},"env":{%q:"1"},%s"args":{"run":[{"args":["-test.run=^TestOneShotPromptHelperProcess$","--","{prompt}"]}]}}`,
			name, filepath.ToSlash(os.Args[0]), oneShotHelperEnv, stdin)
	}
	config := `{"agents":[` + agent("stdinagent", `"promptStdin":[],`) + "," + agent("argvagent", "") + `]}`
	registry := loadDefinitions(t, config)
	return registry
}

// A short prompt to an agent that cannot read stdin still travels as the
// argument it always did.
func TestOneShotPromptKeepsShortPromptsInArgv(t *testing.T) {
	definition, _ := oneShotRegistry(t).Lookup("argvagent")
	call, err := newOneShotPrompt(definition, clidefs.Values{Prompt: "status?"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer call.cleanup()
	if call.Stdin != nil || call.Values.Prompt != "status?" || call.Values.PromptStdin {
		t.Errorf("short prompt call = %+v", call)
	}
}

// A scheduled run's prompt is user-authored and unbounded, so one past the
// Windows command-line limit never travels in argv: an agent that reads stdin
// gets it there, any other agent through a prompt file, and either way the
// agent receives it word for word. A short prompt still travels as the
// argument it always did.
func TestScheduledRunDeliversLongPromptsOffArgv(t *testing.T) {
	long := strings.Repeat("Summarize every open issue and list its labels. ", 700)
	if len(long) <= windowsCommandLineLimit {
		t.Fatalf("test prompt is %d characters, want more than %d", len(long), windowsCommandLineLimit)
	}
	registry := oneShotRegistry(t)
	for _, tc := range []struct {
		agent, prompt string
		// argument, stdin, and file say where the prompt has to arrive.
		argument, stdin, file bool
	}{
		{agent: "stdinagent", prompt: long, stdin: true},
		{agent: "argvagent", prompt: long, file: true},
		{agent: "stdinagent", prompt: "Triage new issues.", argument: true},
		{agent: "argvagent", prompt: "Triage new issues.", argument: true},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.agent, len(tc.prompt)), func(t *testing.T) {
			record := filepath.Join(t.TempDir(), "prompt.md")
			t.Setenv(oneShotHelperRecordEnv, record)
			runner := CommandRunner{Agent: tc.agent, Definitions: registry}
			issue := Issue{Prompt: tc.prompt, ScheduleID: "nightly", Title: "Nightly triage", Repository: "o/r"}
			session := AgentSession{Agent: tc.agent}
			var output strings.Builder
			if err := runner.RunSessionWithOutput(context.Background(), issue, session, nil, &output); err != nil {
				t.Fatalf("dispatch: %v (output %q)", err, output.String())
			}
			want := scheduledRunPrompt(runner, issue, session)
			size := func(on bool) int {
				if on {
					return len(want)
				}
				return 0
			}
			arguments := 0
			if tc.argument || tc.file {
				arguments = 1
			}
			if got, wantLine := strings.TrimSpace(output.String()), fmt.Sprintf("arguments=%d stdin=%d file=%d", arguments, size(tc.stdin), size(tc.file)); got != wantLine {
				t.Errorf("agent reported %q, want %q", got, wantLine)
			}
			got, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != want {
				t.Errorf("agent received %d characters, want the %d-character prompt word for word", len(got), len(want))
			}
		})
	}
}
