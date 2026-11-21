package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lsegal/aviary/internal/config"
)

// resolveAgentArg returns the agent an agent-scoped command acts on: the
// explicitly named agent, or, when none was named, the only configured agent.
// The built-in Projects agent does not count. With several agents an omitted
// name is an error listing them; the CLI never guesses.
func resolveAgentArg(ctx context.Context, name string) (string, error) {
	if name != "" {
		return name, nil
	}
	out, err := dispatcher.CallTool(ctx, "agent_list", nil)
	if err != nil {
		return "", err
	}
	var agents []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &agents); err != nil {
		return "", fmt.Errorf("reading agent list: %w", err)
	}
	var names []string
	for _, a := range agents {
		if a.Name != config.ProjectsAgentName {
			names = append(names, a.Name)
		}
	}
	switch len(names) {
	case 0:
		return "", fmt.Errorf("no agents are configured")
	case 1:
		return names[0], nil
	}
	return "", fmt.Errorf("an agent is required when more than one exists; name one of: %s", strings.Join(names, ", "))
}

// splitAgentArg splits args for a command whose leading agent argument is
// optional: when args holds want values the first is the agent, and when it
// holds one fewer the agent was omitted and resolves to the sole agent.
func splitAgentArg(ctx context.Context, args []string, want int) (string, []string, error) {
	name := ""
	if len(args) == want {
		name, args = args[0], args[1:]
	}
	agent, err := resolveAgentArg(ctx, name)
	return agent, args, err
}
