package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProjectJobSpecsComeFromTheOwningAgent(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{
			{Name: "coder", Model: "cli/claude/opus", Fallbacks: []string{"cli/codex/gpt-5.6-terra:high", "cli/claude/opus"},
				Projects: AgentProjectsConfig{{Target: "o/shared"}}},
			{Name: "api", Model: "anthropic/claude-sonnet-5-5", Fallbacks: []string{"cli/gemini/gemini-3.5-flash"},
				Projects: AgentProjectsConfig{{Target: "o/own"}}},
			{Name: "plain"},
		},
		Models: ModelsConfig{Defaults: &ModelDefaults{Model: "anthropic/claude-sonnet-5-5", Fallbacks: []string{"cli/codex/gpt-5.6-terra"}}},
	}
	assert.Equal(t, "coder", cfg.ProjectOwner("o/shared"))
	assert.Equal(t, "api", cfg.ProjectOwner(" o/own "))
	assert.Equal(t, "", cfg.ProjectOwner("o/unlisted"), "no agent owns an unlisted project")

	assert.Equal(t, []string{"claude/opus", "codex/gpt-5.6-terra:high"}, ProjectJobSpecs(cfg, "coder"), "duplicates drop out")
	assert.Equal(t, []string{"aviary/api", "gemini/gemini-3.5-flash"}, ProjectJobSpecs(cfg, "api"), "an API model runs the job on the agent")
	assert.Equal(t, []string{"aviary/plain", "codex/gpt-5.6-terra"}, ProjectJobSpecs(cfg, "plain"), "no model takes models.defaults")
	assert.Equal(t, []string{"codex/gpt-5.6-terra"}, ProjectJobSpecs(cfg, ""), "no agent takes the CLI models of models.defaults")
	assert.Nil(t, ProjectJobSpecs(cfg, "nobody"))
}

// A CLI provider model is a job spec whether or not it carries the cli/
// prefix, so claude/opus runs jobs on the claude CLI, not on the agent.
func TestProjectJobSpecsTakeCLIProviderModelsInBothForms(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{
			{Name: "coder", Model: "claude/opus", Fallbacks: []string{"cli/claude/opus", "gemini/gemini-3.5-flash", "anthropic/claude-sonnet-5-5"}},
		},
	}
	assert.Equal(t, []string{"claude/opus", "gemini/gemini-3.5-flash", "aviary/coder"}, ProjectJobSpecs(cfg, "coder"))
}

func TestCLIModelsWithoutSessionsOnlyWarn(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{
			{Name: "cline", Model: "cli/cline/m"},
			{Name: "custom", Model: "cli/mytool/m1"},
			{Name: "bogus", Model: "cli/nosuchcli/x"},
		},
		Models: ModelsConfig{CLIDefinitions: map[string]any{"mytool": map[string]any{"binary": "mytool"}}},
	}
	issues := Validate(cfg, nil)
	level := func(field string) string {
		for _, issue := range issues {
			if issue.Field == field {
				return string(issue.Level)
			}
		}
		return ""
	}
	assert.Equal(t, string(LevelWarning), level("agents[0].model"), issues)
	assert.Equal(t, string(LevelWarning), level("agents[1].model"), issues)
	assert.Equal(t, string(LevelError), level("agents[2].model"), issues)
}

// A CLI whose definition resumes its sessions answers agent prompts, built in
// or from models.cli_definitions, so only its installation is checked (#826).
func TestResumableCLIModelsAnswerPrompts(t *testing.T) {
	fakeCLIOnPath(t, "gemini")
	cfg := &Config{
		Agents: []AgentConfig{
			{Name: "gem", Model: "gemini/gemini-3.5-flash"},
			{Name: "custom", Model: "cli/mytool/m1"},
		},
		Models: ModelsConfig{CLIDefinitions: map[string]any{"mytool": map[string]any{
			"binary":  "gemini",
			"session": map[string]any{"assign": "aviary"},
			"output":  map[string]any{"format": "gemini-json"},
			"args": map[string]any{
				"run":    []any{map[string]any{"args": []any{"--session-id", "{session}", "-p", "{prompt}"}}},
				"resume": []any{map[string]any{"args": []any{"--resume", "{session}", "-p", "{prompt}"}}},
			},
		}}},
	}
	assert.Empty(t, Validate(cfg, nil))
	assert.ElementsMatch(t, []string{"gemini", "mytool"}, []string{string(CLIBackends(cfg)[0]), string(CLIBackends(cfg)[1])})
}
