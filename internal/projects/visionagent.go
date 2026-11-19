package projects

import (
	"bytes"
	"context"
	"os"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/browser"
	"github.com/lsegal/aviary/internal/projects/process"
)

// visionAgentRunner adapts the run's agent runner to the one-shot invocation
// browser mode's screenshot fallback makes, so a page Aviary can no longer read
// is described by the same agent the run dispatches issues to.
type visionAgentRunner struct {
	runner CommandRunner
}

// VisionAgent resolves the agent, model, and level to ask, read without
// advancing the round-robin cursor that load balances real issue dispatch.
func (v visionAgentRunner) VisionAgent() browser.AgentSpec {
	spec := v.runner.specForSession(AgentSession{})
	var definition *agents.Definition
	if found, ok := v.runner.definition(spec.Name); ok {
		definition = &found
	}
	return browser.AgentSpec{
		Name:       spec.Name,
		Definition: definition,
		Binary:     v.runner.binary(spec.Name),
		Model:      spec.Model,
		Level:      spec.Level,
		Yolo:       v.runner.Yolo,
	}
}

// RunAgent runs the agent as a tracked child process and returns everything it
// wrote, so a vision call cannot outlive the run that made it.
func (v visionAgentRunner) RunAgent(ctx context.Context, binary string, args []string) (string, error) {
	cmd := newAgentCommand(ctx, binary, args...)
	cmd.Dir = os.TempDir()
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := process.Run(cmd)
	return output.String(), err
}
