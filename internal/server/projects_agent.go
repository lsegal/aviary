package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects"
)

// promptProjectsAgent sends prompt to the built-in Projects agent's hidden
// prompt chat session, the one every board's prompt chat opens, without waiting for the
// answer. Projects routes direct mentions on finished or paused jobs here.
func (s *Server) promptProjectsAgent(_ context.Context, prompt string) error {
	if s.agents == nil {
		return errors.New("the agent manager is not running")
	}
	runner, ok := s.agents.Get(config.ProjectsAgentName)
	if !ok {
		return fmt.Errorf("agent %q is not running", config.ProjectsAgentName)
	}
	sess, err := agent.NewSessionManager().GetOrCreateNamed(config.ProjectsAgentName, config.ProjectsChatSession)
	if err != nil {
		return fmt.Errorf("opening the Projects agent's session: %w", err)
	}
	// The run outlives the engine's poll that routed it.
	ctx := agent.WithSessionID(context.Background(), sess.ID)
	var overrides agent.RunOverrides
	if s.projects != nil {
		overrides.Context = s.projects.ChatContext(ctx, "")
	}
	runner.PromptWithOverrides(ctx, prompt, overrides)
	return nil
}

// runProjectsJobTurn runs one turn of a Projects job on an Aviary agent
// in-process, in the agent's main session as agent_run does, granting the
// turn the job's directory. It returns once the turn ends; cancelling ctx
// stops it.
func (s *Server) runProjectsJobTurn(ctx context.Context, run projects.AviaryAgentRun) error {
	if s.agents == nil {
		return errors.New("the agent manager is not running")
	}
	runner, ok := s.agents.Get(run.Agent)
	if !ok {
		return fmt.Errorf("agent %q not found", run.Agent)
	}
	sess, err := agent.NewSessionManager().GetOrCreateNamed(run.Agent, "")
	if err != nil {
		return fmt.Errorf("opening agent %q's session: %w", run.Agent, err)
	}
	ctx = agent.WithSessionID(ctx, sess.ID)
	if run.Workdir != "" {
		ctx = agent.WithFilesystemGrant(ctx, run.Workdir)
	}
	ctx = agent.WithTurnHold(ctx, run.Hold)
	done := make(chan error, 1)
	finish := func(err error) {
		select {
		case done <- err:
		default:
		}
	}
	history := true
	runner.PromptMediaWithOverrides(ctx, run.Prompt, mediaList(run.MediaURL), agent.RunOverrides{Model: run.Model, History: &history}, func(e agent.StreamEvent) {
		if run.Event != nil {
			run.Event(e)
		}
		switch e.Type {
		case agent.StreamEventDone:
			finish(nil)
		case agent.StreamEventStop:
			finish(context.Canceled)
		case agent.StreamEventError:
			finish(e.Err)
		}
	})
	return <-done
}

// mediaList wraps a single optional media URL as the list a prompt takes.
func mediaList(mediaURL string) []string {
	if mediaURL == "" {
		return nil
	}
	return []string{mediaURL}
}
