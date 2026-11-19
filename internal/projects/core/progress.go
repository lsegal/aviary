package core

import (
	"regexp"
	"strings"
)

// taskListItem matches one GitHub task-list item, such as "- [ ] step" or
// "1. [x] step", capturing its check mark.
var taskListItem = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)])\s+\[([ xX])\](?:\s|$)`)

// ChecklistProgress counts the task-list items in a pull request body and how
// many of them are checked (issue #755). Every item counts the same, wherever
// it sits in the body, and items inside fenced code blocks are ignored. A body
// without task-list items reports a total of zero.
func ChecklistProgress(body string) (done, total int) {
	fence := ""
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fence = trimmed[:3]
			continue
		}
		match := taskListItem.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		total++
		if match[1] != " " {
			done++
		}
	}
	return done, total
}

// ProgressPercent is the share of a plan's tasks that are done, rounded down
// so a plan reads 100% only once every task is checked. It reports false when
// there is no plan to measure against (issue #755).
func ProgressPercent(done, total int) (int, bool) {
	if total <= 0 {
		return 0, false
	}
	done = max(0, min(done, total))
	return done * 100 / total, true
}
