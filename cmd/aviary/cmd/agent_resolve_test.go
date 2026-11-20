package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/scheduler"
)

func taskIDs(s *scheduler.Scheduler) []string {
	var ids []string
	for _, task := range s.ListTasks() {
		ids = append(ids, task.ID)
	}
	return ids
}

func TestTaskCLI_AddOmittedAgentTargetsSoleAgent(t *testing.T) {
	s := setupAgentsCLI(t, "bot")

	_, err := runTaskCmd(t, taskAddCmd, "sole-standup", "Summarize yesterday", "--at", "09:00")
	require.NoError(t, err)
	assert.Contains(t, taskIDs(s), "bot/sole-standup")
}

func TestTaskCLI_AddOmittedAgentWithSeveralAgentsListsThem(t *testing.T) {
	s := setupAgentsCLI(t, "bot", "helper")

	_, err := runTaskCmd(t, taskAddCmd, "multi-standup", "Summarize yesterday", "--at", "09:00")
	assert.EqualError(t, err, "an agent is required when more than one exists; name one of: bot, helper")
	assert.NotContains(t, taskIDs(s), "bot/multi-standup")
	assert.NotContains(t, taskIDs(s), "helper/multi-standup")

	_, err = runTaskCmd(t, taskAddCmd, "helper", "multi-standup", "Summarize yesterday", "--at", "09:00")
	require.NoError(t, err)
	assert.Contains(t, taskIDs(s), "helper/multi-standup")
}

func TestResolveAgentArg(t *testing.T) {
	setupAgentsCLI(t, "bot")
	name, err := resolveAgentArg(t.Context(), "")
	require.NoError(t, err)
	assert.Equal(t, "bot", name)

	// An explicit name is used as given, even one that does not exist.
	name, err = resolveAgentArg(t.Context(), "other")
	require.NoError(t, err)
	assert.Equal(t, "other", name)

	setupAgentsCLI(t)
	_, err = resolveAgentArg(t.Context(), "")
	assert.EqualError(t, err, "no agents are configured")
}

func TestSplitAgentArg(t *testing.T) {
	setupAgentsCLI(t, "bot")

	agent, rest, err := splitAgentArg(t.Context(), []string{"daily"}, 2)
	require.NoError(t, err)
	assert.Equal(t, "bot", agent)
	assert.Equal(t, []string{"daily"}, rest)

	agent, rest, err = splitAgentArg(t.Context(), []string{"helper", "daily"}, 2)
	require.NoError(t, err)
	assert.Equal(t, "helper", agent)
	assert.Equal(t, []string{"daily"}, rest)
}

func TestAgentCLI_OmittedAgentWithSeveralAgents(t *testing.T) {
	setupAgentsCLI(t, "bot", "helper")
	_, err := runTaskCmd(t, agentRunCmd, "hello")
	assert.ErrorContains(t, err, "name one of: bot, helper")
	_, err = runTaskCmd(t, agentStopCmd)
	assert.ErrorContains(t, err, "name one of: bot, helper")
	_, err = runTaskCmd(t, agentTemplateSyncCmd)
	assert.ErrorContains(t, err, "name one of: bot, helper")
	_, err = runTaskCmd(t, configMoveTaskToFileCmd, "daily")
	assert.ErrorContains(t, err, "name one of: bot, helper")
}
