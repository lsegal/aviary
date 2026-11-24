package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestProjectCLIProvidersAreOnlyTheOnesTheProjectUses(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{
			{Name: "coder", Model: "claude/opus", Fallbacks: []string{"anthropic/claude-sonnet-5-5"},
				Projects: AgentProjectsConfig{{Target: "o/r"}}},
			{Name: "other", Model: "opencode/m"},
		},
	}
	assert.Equal(t, []string{"claude"}, ProjectCLIProviders(cfg, "coder", "o/r", nil), "an API fallback runs on the agent, not a CLI")
	assert.Equal(t, []string{"codex"}, ProjectCLIProviders(cfg, "coder", "o/r", []string{"cli/codex:high", "anthropic/claude-sonnet-5-5"}), "unsaved models replace the agent's")
	assert.Empty(t, ProjectCLIProviders(cfg, "coder", "o/r", []string{}))

	cfg.Agents[0].Fallbacks = []string{"cli/gemini/gemini-3.5-flash"}
	cfg.Projects = ProjectsConfig{ChatModel: "codex/gpt-5.6-terra", ChatFallbacks: []string{"cline/m"}, UnblockAgent: "agy/m:high"}
	cfg.Agents[1].Tasks = []TaskConfig{
		{Name: "nightly", Project: "o/r", Model: "cli/muse/m"},
		{Name: "elsewhere", Project: "o/other", Model: "cli/qwen/m"},
		{Name: "default", Project: "o/r"},
	}
	assert.Equal(t, []string{"agy", "claude", "cline", "codex", "gemini", "muse"}, ProjectCLIProviders(cfg, "coder", "o/r", nil),
		"fallbacks, the chat agent, the unblock agent and tasks targeting the project count")
}

func TestProjectModelOverrideWinsOverTheAgent(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{Name: "coder", Model: "cli/claude/sonnet", Fallbacks: []string{"gemini/gemini-3.5-flash"},
			Projects: AgentProjectsConfig{{Target: "o/r"}}}},
	}
	assert.Equal(t, []string{"claude/sonnet", "gemini/gemini-3.5-flash"}, ProjectJobSpecs(cfg, "coder"), "an empty override inherits the agent's")
	assert.Equal(t, "from agent coder", ProjectModelSource(cfg, "coder"))
	assert.Equal(t, "from models.defaults", ProjectModelSource(cfg, ""))

	cfg.Projects.Fallbacks = []string{"codex/gpt-5.6"}
	assert.Equal(t, []string{"claude/sonnet", "gemini/gemini-3.5-flash"}, ProjectJobSpecs(cfg, "coder"), "fallbacks alone override nothing")

	cfg.Projects.Model = "claude:opus"
	assert.Equal(t, []string{"claude/opus", "codex/gpt-5.6"}, ProjectJobSpecs(cfg, "coder"), "the override replaces the agent's model and fallbacks")
	assert.Equal(t, []string{"claude/opus", "codex/gpt-5.6"}, ProjectJobSpecs(cfg, ""), "the override applies to a watch no agent owns")
	assert.Equal(t, "project override", ProjectModelSource(cfg, "coder"))
	assert.Equal(t, []string{"claude", "codex"}, ProjectCLIProviders(cfg, "coder", "o/r", []string{"gemini/gemini-3.5-flash"}),
		"the skills check follows the override, not the agent's models")
}

func TestNormalizeProjectModel(t *testing.T) {
	for in, want := range map[string]string{
		" claude:opus ":      "claude/opus",
		"claude/opus":        "claude/opus",
		"codex/gpt-5.6:high": "codex/gpt-5.6:high",
		"cli/claude/opus":    "cli/claude/opus",
		"nosuch:model":       "nosuch:model",
		"":                   "",
	} {
		assert.Equal(t, want, NormalizeProjectModel(ModelsConfig{}, in), in)
	}
}

func TestValidateProjectModel(t *testing.T) {
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "claude"), []byte("#!/bin/sh\n"), 0o755))
	t.Setenv("PATH", bin)

	assert.NoError(t, ValidateProjectModel(ModelsConfig{}, ""))
	assert.NoError(t, ValidateProjectModel(ModelsConfig{}, "claude:opus"))
	assert.NoError(t, ValidateProjectModel(ModelsConfig{}, "cli/claude/opus"))
	assert.ErrorContains(t, ValidateProjectModel(ModelsConfig{}, "nosuch/model"), `unknown provider "nosuch"`)
	assert.ErrorContains(t, ValidateProjectModel(ModelsConfig{}, "cli/nosuch/model"), `unknown provider "nosuch"`)
	assert.ErrorContains(t, ValidateProjectModel(ModelsConfig{}, "anthropic/claude-sonnet-5-5"), "not a model of a provider reached through a CLI")
	assert.ErrorContains(t, ValidateProjectModel(ModelsConfig{}, "codex/gpt-5.6"), "the codex CLI is not installed")

	assert.ErrorContains(t, ValidateProjectModels(ModelsConfig{}, ProjectsConfig{Fallbacks: []string{"claude/opus"}}), "only with projects.model set")
	assert.ErrorContains(t, ValidateProjectModels(ModelsConfig{}, ProjectsConfig{Model: "claude/opus", Fallbacks: []string{"codex/gpt-5.6"}}), "projects.fallbacks[0]")

	issues := Validate(&Config{Projects: ProjectsConfig{Model: "nosuch/model", Fallbacks: []string{"claude/opus"}}}, nil)
	fields := []string{}
	for _, issue := range issues {
		if issue.Level == LevelError {
			fields = append(fields, issue.Field)
		}
	}
	assert.Equal(t, []string{"projects.model"}, fields, issues)
}
