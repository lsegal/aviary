package projects

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/clidoctor"
	"github.com/lsegal/aviary/internal/projects/agents"
)

// agentVersionTimeout bounds the `binary --version` call made before a
// dispatch. Asking a CLI its version is instant when it works at all, so a
// short deadline is enough, and exceeding it is treated as "version unknown"
// rather than as a failure: the check exists to explain a dispatch that would
// fail anyway, never to stop one that would have worked.
var agentVersionTimeout = 20 * time.Second

// agentVersionCommand reads the binary's reported version. It is a variable so
// tests can answer without an installed CLI.
var agentVersionCommand = clidoctor.ReadVersion

// checkAgentVersion enforces a definition's declared minimum binary version
// before the agent is dispatched. It returns an error naming the agent, the
// version found, and the version required when the installed binary is too
// old, and a warning line when the definition declares a minimum but the
// binary's version cannot be read -- an unreadable version proceeds, because
// blocking on it would break any CLI that prints its version some way aviary
// has not seen. A definition declaring no minimum runs no
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
		return "", definition.VersionTooOldError(binary, clidoctor.FirstLine(reported))
	}
	return "", nil
}
