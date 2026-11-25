package projects

import (
	"fmt"
	"slices"
	"sync"

	"github.com/lsegal/aviary/internal/projects/core"
)

// The kinds of ProjectEvent. The server forwards them to every dashboard on
// /api/ws, so a project added, removed or polled in one place shows in every
// open client at once instead of on its next poll (issue #1211).
const (
	ProjectAdded   = "project-added"
	ProjectRemoved = "project-removed"
	ProjectUpdated = "project-updated"
)

// ProjectEvent is one change to a watched project: added to or removed from
// the watched targets, or its issue count or jobs changed.
type ProjectEvent struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	// Agent is the agent that owns a saved project, empty for a runtime
	// watch no agent owns.
	Agent string `json:"agent,omitempty"`
}

// ProjectObserver receives project events; see SetProjectObserver.
type ProjectObserver func(ProjectEvent)

// SetProjectObserver sets the function every ProjectEvent is sent to. It is
// called from whichever goroutine made the change, so it must not block.
func (m *Manager) SetProjectObserver(observe ProjectObserver) {
	m.projectObserver.Store(&observe)
}

func (m *Manager) emit(events []ProjectEvent) {
	observe := m.projectObserver.Load()
	if observe == nil || *observe == nil {
		return
	}
	for _, ev := range events {
		(*observe)(ev)
	}
}

// watchedLocked is the watched targets and the agent owning each, or nil
// when the settings are rejected. m.mu must be held.
func (m *Manager) watchedLocked() ([]string, map[string]string) {
	settings, err := m.settingsLocked()
	if err != nil {
		return nil, nil
	}
	return settings.Targets, m.owners
}

// changeTargets runs change with m.mu held and emits an event for each
// target it added or removed. A change that leaves the settings rejected
// emits nothing; the next valid change reports the difference.
func (m *Manager) changeTargets(change func()) {
	m.mu.Lock()
	before, beforeOwners := m.watchedLocked()
	change()
	after, afterOwners := m.watchedLocked()
	m.mu.Unlock()
	if before == nil && after == nil {
		return
	}
	var events []ProjectEvent
	for _, target := range after {
		if !slices.Contains(before, target) {
			events = append(events, ProjectEvent{Type: ProjectAdded, Target: target, Agent: afterOwners[target]})
		}
	}
	for _, target := range before {
		if !slices.Contains(after, target) {
			events = append(events, ProjectEvent{Type: ProjectRemoved, Target: target, Agent: beforeOwners[target]})
		}
	}
	if len(events) > 0 {
		m.projectStates.forget(events)
	}
	m.emit(events)
}

// projectStates remembers what the dashboards last saw of each target, so a
// snapshot emits project-updated only for the targets whose counts changed.
type projectStates struct {
	mu   sync.Mutex
	last map[string]string
}

func (p *projectStates) forget(events []ProjectEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ev := range events {
		delete(p.last, ev.Target)
	}
}

// changed records each target's state in snapshot and returns the targets
// whose state differs from the last one recorded.
func (p *projectStates) changed(snapshot core.Snapshot) []string {
	next := make(map[string]string, len(snapshot.Targets))
	for _, target := range snapshot.Targets {
		live := targetLive(&snapshot, target)
		count, counted := snapshot.IssueCounts[target]
		running, waiting := 0, 0
		for _, job := range snapshot.Jobs {
			if job.Target != target {
				continue
			}
			switch job.Status {
			case "active", "stopping":
				running++
			case "waiting", "paused", "queued":
				waiting++
			}
		}
		next[target] = fmt.Sprintf("%t %t %d %d %d", live, counted, count, running, waiting)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, target := range snapshot.Targets {
		if p.last[target] != next[target] {
			out = append(out, target)
		}
	}
	p.last = next
	return out
}
