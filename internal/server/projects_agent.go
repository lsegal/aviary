package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
)

// promptProjectsAgent sends prompt to the built-in Projects agent's main
// session, the one the board's prompt chat opens, without waiting for the
// answer. Projects routes direct mentions on finished or paused jobs here.
func (s *Server) promptProjectsAgent(_ context.Context, prompt string) error {
	if s.agents == nil {
		return errors.New("the agent manager is not running")
	}
	runner, ok := s.agents.Get(config.ProjectsAgentName)
	if !ok {
		return fmt.Errorf("agent %q is not running", config.ProjectsAgentName)
	}
	sess, err := agent.NewSessionManager().GetOrCreate(config.ProjectsAgentName)
	if err != nil {
		return fmt.Errorf("opening the Projects agent's session: %w", err)
	}
	// The run outlives the engine's poll that routed it.
	runner.Prompt(agent.WithSessionID(context.Background(), sess.ID), prompt)
	return nil
}
