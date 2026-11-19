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

	"github.com/lsegal/aviary/internal/projects/agents"
)

const oneShotHelperEnv = "AVIARY_ONESHOT_HELPER"

// promptFilePattern reads the prompt file path back out of the pointer prompt
// an agent without stdin support is given.
var promptFilePattern = regexp.MustCompile(`in the file (.+?)\. Read the whole file`)

// TestOneShotPromptHelperProcess is the stand-in agent: it reports how many
// prompt arguments it got and how large a prompt reached it on stdin or
// through the prompt file, which is all a test needs to see that the agent
// started and was handed the whole prompt.
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
	if len(args) == 1 {
		if match := promptFilePattern.FindStringSubmatch(args[0]); match != nil {
			data, err := os.ReadFile(match[1])
			if err != nil {
				fmt.Printf("cannot read the prompt file: %v\n", err)
				os.Exit(1)
			}
			file = len(data)
		}
	}
	fmt.Printf("arguments=%d stdin=%d file=%d\n", len(args), len(stdin), file)
	os.Exit(0)
}

// Every out-of-band call is marked one-shot, so an agent definition can add
// the flags a call outside any checkout needs, such as Codex's
// --skip-git-repo-check.
func TestOneShotPromptMarksTheCallOneShot(t *testing.T) {
	codex, _ := agents.MustBuiltin().Lookup("codex")
	call, err := newOneShotPrompt(codex, agents.Values{Prompt: "status?"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer call.cleanup()
	want := []string{"exec", "--json", "--skip-git-repo-check", "-"}
	if got := codex.Render(agents.ModeRun, call.Values); !reflect.DeepEqual(got, want) {
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
func oneShotRegistry(t *testing.T) *agents.Registry {
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
	call, err := newOneShotPrompt(definition, agents.Values{Prompt: "status?"}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer call.cleanup()
	if call.Stdin != nil || call.Values.Prompt != "status?" || call.Values.PromptStdin {
		t.Errorf("short prompt call = %+v", call)
	}
}
