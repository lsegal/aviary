package projects

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lsegal/aviary/internal/projects/core"
)

// maxVisibleJobs is how many jobs a compact status view lists before it
// summarizes the rest.
const maxVisibleJobs = 6

// JobSnapshot and EngineSnapshot live in package core so the API and later
// dashboards render the same published state.
type JobSnapshot = core.JobSnapshot

type EngineSnapshot = core.Snapshot

func formatInterval(d time.Duration) string { return core.FormatInterval(d) }

// jobStatusRank orders jobs for display: running work first, then failures,
// then everything else, with completed jobs last.
func jobStatusRank(status string) int {
	switch status {
	case "active", "waiting":
		return 0
	case "failed":
		return 1
	case "complete":
		return 3
	default:
		return 2
	}
}

// sortJobSnapshots orders jobs by jobStatusRank, breaking ties within a rank
// by most recently started first.
func sortJobSnapshots(jobs []JobSnapshot) {
	slices.SortFunc(jobs, func(a, b JobSnapshot) int {
		if rankDiff := jobStatusRank(a.Status) - jobStatusRank(b.Status); rankDiff != 0 {
			return rankDiff
		}
		return b.Started.Compare(a.Started)
	})
}

// publishedJobs lists every in-flight job for the published snapshot, sorted
// the way status views list them.
func publishedJobs(jobs map[string]JobSnapshot) []JobSnapshot {
	list := make([]JobSnapshot, 0, len(jobs))
	for _, job := range jobs {
		list = append(list, job)
	}
	sortJobSnapshots(list)
	return list
}

// truncate shortens s to at most width runes, ending in "..." when it was cut.
func truncate(s string, width int) string {
	if width < 1 {
		return ""
	}
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	runes := []rune(s)
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

// deliveryText describes how the engine hears about GitHub changes and when it
// last checked GitHub.
func deliveryText(snapshot EngineSnapshot) string {
	text := "polling every " + formatInterval(snapshot.Interval)
	if snapshot.UseWebhooks {
		text = "push"
		if snapshot.WebhookURL != "" {
			text += " " + snapshot.WebhookURL
		}
		if !snapshot.WebhookOnline {
			text += " (offline)"
		}
	}
	if !snapshot.LastPoll.IsZero() {
		text += "; checked " + snapshot.LastPoll.Format("15:04:05")
	}
	return text
}

// jobAgentSummary names the agent, model and effort a job runs with.
func jobAgentSummary(job JobSnapshot) string {
	if job.Agent == "" {
		return "pending"
	}
	summary := job.Agent
	if job.Model != "" {
		summary += " (" + job.Model
		if job.Effort != "" {
			summary += ", " + job.Effort
		}
		summary += ")"
	} else if job.Effort != "" {
		summary += " (" + job.Effort + ")"
	}
	return summary
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func formatTargets(targets []string, counts map[string]int) []string {
	result := make([]string, len(targets))
	for i, target := range targets {
		result[i] = fmt.Sprintf("%s (%d issues)", target, counts[target])
	}
	return result
}
