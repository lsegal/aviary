package projects

import (
	"slices"
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

// EngineSnapshot is the engine's published state.
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
