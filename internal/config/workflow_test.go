package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func workflowAgent(t TaskConfig) *Config {
	return &Config{Agents: []AgentConfig{{
		Name:     "bot",
		Channels: []ChannelConfig{{Type: "slack", ID: "team", Name: "Team"}},
		Tasks:    []TaskConfig{t},
	}}}
}

func errorMessages(issues []Issue) []string {
	var out []string
	for _, iss := range issues {
		if iss.Level == LevelError {
			out = append(out, iss.Message)
		}
	}
	return out
}

func TestValidateWorkflows(t *testing.T) {
	ok := TaskConfig{Name: "triage", Trigger: TriggerMessage, Channel: "Team", Entry: "C1", Match: "*crash*", Prompt: "p", Target: ReplyTarget}
	assert.Empty(t, errorMessages(Validate(workflowAgent(ok), nil)))
	assert.False(t, hasIssue(Validate(workflowAgent(ok), nil), "never be triggered"))

	missing := ok
	missing.Channel = "Other"
	assert.True(t, hasIssue(Validate(workflowAgent(missing), nil), `no channel named "Other"`))

	scheduled := ok
	scheduled.Every = "1h"
	assert.True(t, hasIssue(Validate(workflowAgent(scheduled), nil), "cannot also have a schedule"))

	notWorkflow := TaskConfig{Name: "t", Every: "1h", Prompt: "p", Match: "*"}
	assert.True(t, hasIssue(Validate(workflowAgent(notWorkflow), nil), "only supported for workflows"))

	reply := TaskConfig{Name: "t", Every: "1h", Prompt: "p", Target: ReplyTarget}
	assert.True(t, hasIssue(Validate(workflowAgent(reply), nil), "only workflows can reply"))
}

func TestWorkflowMarkdownRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := TaskConfig{Name: "triage", Trigger: TriggerMessage, Channel: "Team", Entry: "C1", Match: "*crash*", Prompt: "Triage it.", Target: ReplyTarget, Model: "anthropic/claude-sonnet-5-5"}
	path, err := SaveMarkdownTask(dir, in)
	require.NoError(t, err)
	out, err := LoadMarkdownTask(path)
	require.NoError(t, err)
	assert.True(t, out.IsWorkflow())
	assert.Equal(t, "Team", out.Channel)
	assert.Equal(t, "C1", out.Entry)
	assert.Equal(t, "*crash*", out.Match)
	assert.Equal(t, ReplyTarget, out.Target)
	assert.Equal(t, "anthropic/claude-sonnet-5-5", out.Model)
}
