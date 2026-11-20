package projects

import (
	"bytes"
	"context"
	"os"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/process"
	"github.com/lsegal/aviary/internal/projects/browser"
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
	var definition *clidefs.Definition
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

// RunInProcess runs the vision turn through core's agent runner outside any
// checkout.
func (v visionAgentRunner) RunInProcess(ctx context.Context, spec browser.AgentSpec, prompt, mediaURL string) (string, error) {
	var output bytes.Buffer
	err := v.runner.runInProcess(ctx, agentSpec{Name: spec.Name, Model: spec.Model, Level: spec.Level}, prompt, mediaURL, os.TempDir(), nil, &output, nil)
	return output.String(), err
}
