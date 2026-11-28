package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJobCLI_ListAndLogs(t *testing.T) {
	s := setupTaskCLI(t)

	out, err := runTaskCmd(t, jobListCmd)
	require.NoError(t, err)
	assert.Contains(t, out, "No jobs.")

	standup, err := s.Queue().Enqueue("bot/standup", "bot", "Summarize yesterday", "", 0, "", "")
	require.NoError(t, err)
	digest, err := s.Queue().Enqueue("bot/digest", "bot", "Send the digest", "", 0, "", "")
	require.NoError(t, err)

	out, err = runTaskCmd(t, jobListCmd)
	require.NoError(t, err)
	assert.Contains(t, out, "JOB")
	assert.Contains(t, out, standup.ID)
	assert.Contains(t, out, digest.ID)

	out, err = runTaskCmd(t, jobListCmd, "--task", "standup")
	require.NoError(t, err)
	assert.Contains(t, out, standup.ID)
	assert.NotContains(t, out, digest.ID)

	out, err = runTaskCmd(t, jobListCmd, "--task", "bot/digest", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"id": "`+digest.ID+`"`)
	assert.NotContains(t, out, standup.ID)

	out, err = runTaskCmd(t, jobLogsCmd, standup.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, out)
	assert.NotContains(t, out, "not yet implemented")

	_, err = runTaskCmd(t, jobLogsCmd, "missing-job")
	assert.ErrorContains(t, err, "job missing-job not found")
}
