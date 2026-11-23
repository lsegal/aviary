package projects

import (
	"fmt"
	"slices"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects/core"
)

// recentScheduleRuns is how many of a schedule's runs its board entry lists.
const recentScheduleRuns = 20

// boardEntry is one item the board lists: a job, or a schedule with its runs
// collapsed into one entry, so a schedule never shows as a card per fire.
type boardEntry struct {
	// job is the job itself, or a schedule's latest run.
	job core.JobSnapshot
	// state is job's board state, or the schedule's; see scheduleState.
	state string
	// runs are a schedule's runs, newest first; nil for any other job.
	runs []core.JobSnapshot
}

// boardEntries collapses each schedule's runs in jobs into one entry, placed
// where its first run is, and keeps every other job as its own entry.
func boardEntries(jobs []core.JobSnapshot) []boardEntry {
	var entries []boardEntry
	index := map[string]int{}
	for _, job := range jobs {
		if job.Kind != core.JobKindSchedule || job.ScheduleID == "" {
			entries = append(entries, boardEntry{job: job, state: job.BoardState()})
			continue
		}
		key := job.Target + "|" + job.ScheduleID
		i, ok := index[key]
		if !ok {
			index[key] = len(entries)
			entries = append(entries, boardEntry{job: job, runs: []core.JobSnapshot{job}})
			continue
		}
		entries[i].runs = append(entries[i].runs, job)
	}
	for i := range entries {
		if entries[i].runs == nil {
			continue
		}
		runs := entries[i].runs
		slices.SortFunc(runs, func(a, b core.JobSnapshot) int { return b.Number - a.Number })
		entries[i].job, entries[i].state = runs[0], scheduleState(runs)
	}
	return entries
}

// runFinished reports whether a scheduled run has ended.
func runFinished(run core.JobSnapshot) bool {
	return run.Status == "complete" || run.Status == "failed"
}

// scheduleState is the board state of a schedule with runs, newest first: its
// latest run's while that run is in flight, failed while the last run failed
// and was not dismissed, and scheduled otherwise, so a finished run is never
// done.
func scheduleState(runs []core.JobSnapshot) string {
	latest := runs[0]
	switch {
	case !runFinished(latest):
		return latest.BoardState()
	case latest.Status == "failed" && !latest.Dismissed:
		return core.BoardFailed
	}
	return core.BoardScheduled
}

// runResult is how a finished scheduled run ended: succeeded, failed, or
// fallback when its precompiled script failed and the model ran it instead.
func runResult(run core.JobSnapshot) string {
	switch {
	case run.Status == "failed":
		return "failed"
	case run.Fallback:
		return "fallback"
	}
	return "succeeded"
}

// scheduleRun is one scheduled run as a schedule's entry lists it.
func scheduleRun(run core.JobSnapshot) map[string]any {
	entry := map[string]any{
		"key":    fmt.Sprintf("%s#%d", run.Target, run.Number),
		"number": run.Number,
		"status": run.Status,
		"state":  run.BoardState(),
	}
	if !run.Started.IsZero() {
		entry["started"] = run.Started
	}
	if runFinished(run) {
		entry["result"] = runResult(run)
		if run.Summary != "" {
			entry["summary"] = run.Summary
		}
		if !run.Finished.IsZero() {
			entry["finished"] = run.Finished
			if !run.Started.IsZero() {
				entry["duration_ms"] = run.Finished.Sub(run.Started).Milliseconds()
			}
		}
	}
	return entry
}

// addScheduleRuns adds a schedule entry's last_run, recent_runs and, when
// tasks has its schedule, next_run to summary.
func addScheduleRuns(summary map[string]any, entry boardEntry, tasks map[string]domain.ScheduledTask) {
	recent := make([]map[string]any, 0, min(len(entry.runs), recentScheduleRuns))
	for _, run := range entry.runs[:min(len(entry.runs), recentScheduleRuns)] {
		recent = append(recent, scheduleRun(run))
	}
	summary["recent_runs"] = recent
	for _, run := range entry.runs {
		if runFinished(run) {
			last := scheduleRun(run)
			if run.Dismissed {
				last["dismissed"] = true
			}
			summary["last_run"] = last
			break
		}
	}
	if task, ok := tasks[entry.job.ScheduleID]; ok {
		summary["next_run"] = scheduleNextRun(task)
	}
}

// scheduleNextRun is what comes next for task: its next fire, whether it is
// paused, and whether a fire is queued: waiting, under the task's overlap
// policy, for a run still in flight.
func scheduleNextRun(task domain.ScheduledTask) map[string]any {
	next := map[string]any{"paused": !task.Enabled}
	if task.NextRun != nil && task.Enabled {
		next["at"] = *task.NextRun
	}
	active, waiting := 0, false
	for _, run := range task.History {
		switch run.Status {
		case domain.TaskRunQueued, domain.TaskRunRetrying:
			active++
			waiting = true
		case domain.TaskRunRunning:
			active++
		}
	}
	next["queued"] = waiting && active > 1
	return next
}
