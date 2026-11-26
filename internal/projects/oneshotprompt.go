package projects

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/lsegal/aviary/internal/clidefs"
)

// oneShotArgvPromptLimit is the largest prompt an out-of-band call still
// passes as an argument to an agent that cannot read it from stdin, and the
// largest a job run passes as an argument at all. Anything larger goes on
// stdin or through a file, well clear of Windows' 32,767-character command
// line and Linux's 128 KiB single-argument limit.
const oneShotArgvPromptLimit = 8 * 1024

// windowsCommandLineLimit is the most characters CreateProcess accepts in a
// command line, including the executable; past it Windows fails the spawn with
// "The filename or extension is too long".
const windowsCommandLineLimit = 32767

// linuxArgumentLimit is MAX_ARG_STRLEN, the longest single argument execve
// accepts on Linux.
const linuxArgumentLimit = 128 * 1024

// oneShotPrompt is how an out-of-band call hands its prompt to the agent.
type oneShotPrompt struct {
	// Values are the render values, with Prompt or PromptStdin set.
	Values clidefs.Values
	// Stdin is the prompt itself when the agent reads it from stdin.
	Stdin io.Reader
	// cleanup removes the prompt file, when one was written.
	cleanup func()
}

// newOneShotPrompt decides how a one-shot prompt reaches the agent. The
// prompt agent, the MCP ask tool, issue creation, and the unblock agent all
// carry assembled instance context that grows with the number of jobs and
// their scrollback, so it never travels in argv when the agent can read stdin,
// and travels through a file in dir when it is too large for argv otherwise.
func newOneShotPrompt(definition clidefs.Definition, values clidefs.Values, dir string) (oneShotPrompt, error) {
	call := oneShotPrompt{Values: values, cleanup: noPromptFileCleanup}
	call.Values.OneShot = true
	call.Values.NoCheckout = true
	if definition.ReadsPromptFromStdin() {
		call.Values.PromptStdin = true
		call.Stdin = strings.NewReader(values.Prompt)
		return call, nil
	}
	if len(values.Prompt) <= oneShotArgvPromptLimit {
		return call, nil
	}
	return call.writePromptFile(dir)
}

// newRunPrompt decides how a dispatched or resumed job's prompt reaches the
// agent. A gh-fix dispatch or a recovery is short and still travels as the
// argument it always did, but a scheduled run carries its user-authored
// prompt, which has no bound: one too large for argv goes on stdin when the
// mode's template can take it there, and through a file in dir otherwise.
func newRunPrompt(definition clidefs.Definition, mode clidefs.Mode, values clidefs.Values, dir string) (oneShotPrompt, error) {
	call := oneShotPrompt{Values: values, cleanup: noPromptFileCleanup}
	if len(values.Prompt) <= oneShotArgvPromptLimit {
		return call, nil
	}
	if definition.ReadsPromptFromStdinIn(mode) {
		call.Values.PromptStdin = true
		call.Stdin = strings.NewReader(values.Prompt)
		return call, nil
	}
	return call.writePromptFile(dir)
}

// noPromptFileCleanup is the cleanup for a prompt that travels in argv or on
// stdin: no prompt file was written, so there is nothing to remove.
func noPromptFileCleanup() {
	// Intentionally empty: only writePromptFile creates something to clean up.
}

// writePromptFile writes the prompt to a file in dir and replaces it with a
// pointer to that file.
func (call oneShotPrompt) writePromptFile(dir string) (oneShotPrompt, error) {
	file, err := os.CreateTemp(dir, "aviary-prompt-*.md")
	if err != nil {
		return call, fmt.Errorf("write the prompt file: %w", err)
	}
	path := file.Name()
	_, writeErr := file.WriteString(call.Values.Prompt)
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(path)
		return call, fmt.Errorf("write the prompt file: %w", writeErr)
	}
	call.cleanup = func() { _ = os.Remove(path) }
	call.Values.Prompt = fmt.Sprintf("Your full instructions are in the file %s. Read the whole file first, then follow it exactly and answer in the format it asks for.", path)
	return call, nil
}

// spawnError explains a failure to start an agent process with the measured
// sizes of the invocation (never its contents) and the OS limit they exceed,
// so a spawn failure names its cause instead of only the OS error string.
func spawnError(cmd *exec.Cmd, err error) error {
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	envBytes := 0
	for _, entry := range env {
		envBytes += len(entry) + 1
	}
	commandLine, longest := 0, 0
	for _, arg := range cmd.Args {
		commandLine += len(arg) + 1
		if strings.ContainsAny(arg, " \t\"") || arg == "" {
			commandLine += 2 + strings.Count(arg, `"`)
		}
		longest = max(longest, len(arg))
	}
	sizes := fmt.Sprintf("%d arguments, command line about %d characters, longest argument %d characters, environment %d bytes in %d variables, executable path %d characters, working directory %s",
		len(cmd.Args), commandLine, longest, envBytes, len(env), len(cmd.Path), cmd.Dir)
	return fmt.Errorf("starting %s failed: %w (%s; %s)", cmd.Path, err, sizes, spawnLimit(runtime.GOOS, commandLine, longest, len(cmd.Path)))
}

// spawnLimit names the OS limit the measured invocation exceeds.
func spawnLimit(goos string, commandLine, longest, path int) string {
	switch {
	case goos == "windows" && commandLine > windowsCommandLineLimit:
		return fmt.Sprintf("exceeds the Windows command-line limit of %d characters", windowsCommandLineLimit)
	case goos == "windows" && path >= 260:
		return "the executable path exceeds the Windows MAX_PATH limit of 260 characters"
	case goos == "linux" && longest > linuxArgumentLimit:
		return fmt.Sprintf("an argument exceeds the Linux single-argument limit of %d bytes", linuxArgumentLimit)
	}
	return "no known size limit is exceeded"
}
