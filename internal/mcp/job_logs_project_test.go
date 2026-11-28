package mcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/store"
)

// job_logs shows a project task run's Code Loop output, not just its
// summary, and says why when there is none (issue #1646).
func TestJobLogsShowsProjectTaskRunOutput(t *testing.T) {
	d, m := cfpSetupWatchedProjects(t)
	summary := "Filed a bug (#1656) and an end-to-end test issue (#1657)."
	m.Snapshot(core.Snapshot{Targets: []string{"acme/app"}, LastPoll: time.Now(), Jobs: []core.JobSnapshot{
		{Target: "acme/app", Number: 1795846709, Kind: core.JobKindSchedule, ScheduleID: "bot/qa", RunID: "job_qa", Status: "complete", Started: time.Now(), Log: "Running: gh issue create\n" + summary + "\n"},
		{Target: "acme/app", Number: 1795846800, Kind: core.JobKindSchedule, ScheduleID: "bot/qa", RunID: "job_quiet", Status: "complete", Started: time.Now()},
		{Target: "acme/app", Number: 1795846900, Kind: core.JobKindSchedule, ScheduleID: "bot/qa", RunID: "job_cut", Status: "complete", Started: time.Now(), Log: "last lines\n", LogTruncated: true},
	}})
	write := func(job domain.Job) string {
		job.AgentID, job.TaskID, job.TaskType = "bot", "bot/qa", "project"
		require.NoError(t, store.WriteJSON(store.JobPath("bot", job.ID), job))
		return tcCall(t, d, "job_logs", map[string]any{"id": job.ID})
	}

	assert.Equal(t, "Running: gh issue create\n"+summary+"\n",
		write(domain.Job{ID: "job_qa", Status: domain.JobStatusCompleted, Output: summary}))
	assert.Equal(t, "[Earlier output was truncated; showing the most recent output.]\nlast lines\n\nSummary: done",
		write(domain.Job{ID: "job_cut", Status: domain.JobStatusCompleted, Output: "done"}))
	assert.Equal(t, "No transcript for this run: its Code Loop run recorded no output.\n\nSummary: done",
		write(domain.Job{ID: "job_quiet", Status: domain.JobStatusCompleted, Output: "done"}))
	assert.Equal(t, "No transcript for this run: Code Loop has no record of its run, which was cleared, expired under the project's finished_ttl, or never started.\n\nSummary: done",
		write(domain.Job{ID: "job_gone", Status: domain.JobStatusCompleted, Output: "done"}))
	assert.Equal(t, "Precompiled script failed, so the model ran the task:\nboom\n\nRunning: gh issue create\n"+summary+"\n",
		write(domain.Job{ID: "job_qa", Status: domain.JobStatusCompleted, Output: summary, ScriptError: "boom"}))
	// A run Code Loop has not started yet shows what the job has.
	assert.Equal(t, "(no output captured)", write(domain.Job{ID: "job_waiting", Status: domain.JobStatusPending}))

	full := GetDeps()
	SetDeps(&Deps{Agents: full.Agents, Scheduler: full.Scheduler})
	t.Cleanup(func() { SetDeps(full) })
	assert.Equal(t, "No transcript for this run: Code Loop is not running on this server.\n\nSummary: done",
		write(domain.Job{ID: "job_qa", Status: domain.JobStatusCompleted, Output: "done"}))
}
