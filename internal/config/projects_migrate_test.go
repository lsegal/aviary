package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadMigratesProjectCLIListsOntoAgents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`agents:
  - name: helper
    model: anthropic/claude-sonnet-5-5
projects:
  agents: [claude/opus, codex:high]
  list:
    - target: o/shared
    - target: https://github.com/o/own
      agents: [gemini/gemini-3-pro, claude]
    - target: o/same
      agents: [claude/opus, codex:high]
    - target: o/onagent
      agents: [aviary/helper]
`), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)

	assert.Empty(t, cfg.Projects.LegacyAgents)
	assert.Equal(t, ProjectsJobsAgentName, cfg.Projects.Agent)
	agents := map[string]AgentConfig{}
	for _, a := range cfg.Agents {
		agents[a.Name] = a
	}
	assert.Equal(t, "cli/claude/opus", agents[ProjectsJobsAgentName].Model)
	assert.Equal(t, []string{"cli/codex/gpt-5.6-terra:high"}, agents[ProjectsJobsAgentName].Fallbacks)
	assert.Equal(t, "cli/gemini/gemini-3-pro", agents["project-o-own"].Model)
	assert.Equal(t, []string{"cli/claude/sonnet"}, agents["project-o-own"].Fallbacks)
	assert.Len(t, cfg.Agents, 3, "a list matching an existing agent reuses it")

	want := map[string]string{"o/shared": "", "https://github.com/o/own": "project-o-own", "o/same": ProjectsJobsAgentName, "o/onagent": "helper"}
	for _, project := range cfg.Projects.List {
		assert.Empty(t, project.LegacyAgents)
		assert.Equal(t, want[project.Target], project.Agent, project.Target)
	}

	// The jobs run on the same specs the lists named, with the CLI's default
	// model filled in where they named none.
	assert.Equal(t, []string{"claude/opus", "codex/gpt-5.6-terra:high"}, ProjectJobSpecs(cfg, cfg.Projects.AgentFor("o/shared")))
	assert.Equal(t, []string{"gemini/gemini-3-pro", "claude/sonnet"}, ProjectJobSpecs(cfg, cfg.Projects.AgentFor("https://github.com/o/own")))
	assert.Equal(t, []string{"aviary/helper"}, ProjectJobSpecs(cfg, cfg.Projects.AgentFor("o/onagent")))

	// Saving writes the migrated form, which loads unchanged.
	require.NoError(t, Save(path, cfg))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "agents: [")
	again, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, cfg.Projects, again.Projects)
	assert.Len(t, again.Agents, 3)
}

func TestLoadMigratesTheOldDefaultCLI(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	require.NoError(t, os.WriteFile(path, []byte("projects:\n  list:\n    - target: o/r\n"), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, ProjectsJobsAgentName, cfg.Projects.Agent)
	assert.Equal(t, []string{"codex/gpt-5.6-terra"}, ProjectJobSpecs(cfg, cfg.Projects.AgentFor("o/r")))
}

func TestMigrateLeavesAttachedProjectsAlone(t *testing.T) {
	cfg := &Config{
		Agents:   []AgentConfig{{Name: "coder", Model: "cli/claude/opus"}},
		Projects: ProjectsConfig{List: []ProjectConfig{{Target: "o/r", Agent: "coder"}}},
	}
	assert.False(t, MigrateProjectAgents(cfg))
	assert.Empty(t, cfg.Projects.Agent)
	assert.Len(t, cfg.Agents, 1)
}

func TestMigratedAgentNamesAvoidTakenNames(t *testing.T) {
	cfg := &Config{
		Agents:   []AgentConfig{{Name: ProjectsJobsAgentName, Model: "cli/claude/haiku"}},
		Projects: ProjectsConfig{LegacyAgents: []string{"claude/opus"}},
	}
	assert.True(t, MigrateProjectAgents(cfg))
	assert.Equal(t, ProjectsJobsAgentName+"-2", cfg.Projects.Agent)
	assert.Equal(t, "projects-jobs", projectAgentName(""))
	assert.Equal(t, "project-octo-repo", projectAgentName("https://github.com/Octo/Repo"))
}

func TestMigratedCLIModelsBeyondClaudeAndCodexOnlyWarn(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{
			{Name: "gem", Model: "cli/gemini/gemini-3.5-flash"},
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
