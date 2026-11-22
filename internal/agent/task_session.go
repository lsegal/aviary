package agent

import (
	"fmt"
	"strings"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// EnsureTaskSession returns the session a task's runs share, creating it when
// the task has not run yet. The session is named after the task and titled
// with the task's name, so it shows the task everywhere instead of "New chat".
func EnsureTaskSession(agentID, taskName string) (*domain.Session, error) {
	taskName = strings.TrimSpace(taskName)
	if taskName == "" {
		return nil, fmt.Errorf("task name is required")
	}
	sm := NewSessionManager()
	sess, err := sm.GetOrCreateNamedTyped(agentID, taskName, domain.SessionTypeTask)
	if err != nil {
		return nil, err
	}
	if SessionTitle(agentID, sess.ID) != taskName {
		if err := sm.Rename(agentID, sess.ID, taskName); err != nil {
			return nil, err
		}
	}
	sess.Title = taskName
	return sess, nil
}

// TaskSessionOwner returns the index in a.Tasks of the task whose runs share
// sess, or -1. A task session whose task was deleted, or renamed before
// renames carried sessions along, has no owner and stays a regular chat.
func TaskSessionOwner(a config.AgentConfig, sess *domain.Session) int {
	if sess == nil || sess.Type != domain.SessionTypeTask {
		return -1
	}
	for i, t := range a.Tasks {
		if t.Name != "" && t.Name == sess.Name {
			return i
		}
	}
	return -1
}

// RenameTaskSession moves a task's session to its new name and retitles it,
// so a renamed task keeps its run history. It does nothing when the task has
// no session yet, and leaves an unrelated session already holding the new
// name alone.
func RenameTaskSession(agentID, from, to string) error {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || from == to {
		return nil
	}
	sm := NewSessionManager()
	if store.FindSessionPath(agentID, from) == "" {
		return nil
	}
	sess, err := sm.Load(agentID, from)
	if err != nil {
		return err
	}
	if sess.Type != domain.SessionTypeTask {
		return nil
	}
	if store.FindSessionPath(agentID, to) != "" {
		return fmt.Errorf("session %q already exists", to)
	}
	moved, err := sm.Rekey(agentID, sess.ID, to)
	if err != nil {
		return err
	}
	return sm.Rename(agentID, moved.ID, to)
}
