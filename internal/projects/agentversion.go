package projects

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/process"
)

// agentVersionTimeout bounds the `binary --version` call made before a
// dispatch. Asking a CLI its version is instant when it works at all, so a
// short deadline is enough, and exceeding it is treated as "version unknown"
// rather than as a failure: the check exists to explain a dispatch that would
// fail anyway, never to stop one that would have worked.
var agentVersionTimeout = 20 * time.Second

// agentVersionCommand reads the binary's reported version. It is a variable so
// tests can answer without an installed CLI.
var agentVersionCommand = func(ctx context.Context, binary string) ([]byte, error) {
	return process.CombinedOutput(exec.CommandContext(ctx, binary, "--version"))
}

// checkAgentVersion enforces a definition's declared minimum binary version
// before the agent is dispatched. It returns an error naming the agent, the
// version found, and the version required when the installed binary is too
// old, and a warning line when the definition declares a minimum but the
// binary's version cannot be read -- an unreadable version proceeds, because
// blocking on it would break any CLI that prints its version some way glorp
// has not seen (issue #535). A definition declaring no minimum runs no
// process at all.
func checkAgentVersion(ctx context.Context, definition agents.Definition, binary string) (warning string, err error) {
	if strings.TrimSpace(definition.MinVersion) == "" {
		return "", nil
	}
	versionCtx, cancel := context.WithTimeout(ctx, agentVersionTimeout)
	defer cancel()
	output, runErr := agentVersionCommand(versionCtx, binary)
	reported := strings.TrimSpace(string(output))
	if runErr != nil {
		return fmt.Sprintf("Warning: could not read %s's version (%v); %s requires %s %s or newer.", binary, runErr, definition.Name, definition.Binary, definition.MinVersion), nil
	}
	supported, comparable := definition.VersionSupported(reported)
	if !comparable {
		return fmt.Sprintf("Warning: could not read a version from %s --version; %s requires %s %s or newer.", binary, definition.Name, definition.Binary, definition.MinVersion), nil
	}
	if !supported {
		return "", definition.VersionTooOldError(binary, firstLine(reported))
	}
	return "", nil
}

// firstLine is the part of a --version output worth quoting back: CLIs that
// print a banner or an update notice alongside the number would otherwise
// paste all of it into the error.
func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}
