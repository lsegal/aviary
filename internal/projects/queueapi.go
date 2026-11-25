package projects

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/lsegal/aviary/internal/projects/core"
)

// queueTarget resolves the project a queue call names, or the only watched
// project when it names none.
func queueTarget(engine *Engine, target string) (string, error) {
	target = strings.TrimSpace(target)
	targets := engine.Targets
	if len(targets) == 0 && engine.Repo != "" {
		targets = []string{engine.Repo}
	}
	if target == "" {
		if len(targets) != 1 {
			return "", errors.New("name the project to queue on")
		}
		return targets[0], nil
	}
	if !slices.Contains(targets, target) {
		return "", fmt.Errorf("%s is not watched", target)
	}
	return target, nil
}

// QueueJobs is project_job_queue: queue pull requests for gh-safe-merge or
// Discussion threads for gh-discuss as board jobs, in the order given, or ask
// for issues to be fixed with gh-fix. A number a job already owns is reported
// with that job instead of being queued twice.
func (m *Manager) QueueJobs(ctx context.Context, target, repository, skill string, numbers []int, titles map[int]string) (map[string]any, error) {
	engine := m.engine()
	if engine == nil {
		return nil, ErrNotWatching
	}
	target, err := queueTarget(engine, target)
	if err != nil {
		return nil, err
	}
	if skill == SkillFix {
		return m.queueFixes(ctx, target, repository, numbers)
	}
	outcomes, err := engine.QueueJobs(ctx, QueueRequest{Target: target, Repository: repository, Skill: skill, Numbers: numbers, Titles: titles})
	if err != nil {
		if errors.Is(err, core.ErrNotReady) {
			return nil, errors.New("the engine is still starting; try again")
		}
		return nil, err
	}
	return map[string]any{"target": target, "skill": skill, "results": outcomes}, nil
}

// queueFixes answers gh-fix for numbers: an issue a job already owns is
// reported with that job, a finished job is retried, and an issue with no job
// yet starts a poll, which dispatches it when it matches the project's issue
// filter. On a manual-mode project, which is never searched, an issue with
// no job yet is requested and dispatched whatever the filter.
func (m *Manager) queueFixes(ctx context.Context, target, repository string, numbers []int) (map[string]any, error) {
	if len(numbers) == 0 {
		return nil, errors.New("name at least one issue to fix")
	}
	engine := m.engine()
	var outcomes []QueueOutcome
	polled := false
	for _, number := range numbers {
		key := fmt.Sprintf("%s#%d", target, number)
		job, err := findJob(m.currentSnapshot().Jobs, target, number)
		switch {
		case err == nil && job.Kind == core.JobKindIssue && slices.Contains(ownedStatuses, job.Status):
			outcomes = append(outcomes, QueueOutcome{Number: number, OwnedBy: key, Reason: "already owned by job " + key})
		case err == nil && job.Kind == core.JobKindIssue:
			if _, err := m.JobAction(ctx, "retry", target, number); err != nil {
				outcomes = append(outcomes, QueueOutcome{Number: number, Reason: err.Error()})
				continue
			}
			outcomes = append(outcomes, QueueOutcome{Number: number, Key: key, Queued: true, Reason: "retried its finished job"})
		case err == nil:
			outcomes = append(outcomes, QueueOutcome{Number: number, OwnedBy: key, Reason: fmt.Sprintf("job %s is a %s job", key, job.Kind)})
		case engine.Manual.Has(target):
			if err := engine.RequestFix(target, repository, number); err != nil {
				outcomes = append(outcomes, QueueOutcome{Number: number, Reason: err.Error()})
				continue
			}
			outcomes = append(outcomes, QueueOutcome{Number: number, Key: key, Queued: true, Reason: "requested; dispatching it now"})
		default:
			if !polled {
				engine.nudgePoll()
				polled = true
			}
			outcomes = append(outcomes, QueueOutcome{Number: number, Reason: "no job yet; checking GitHub now, which dispatches it if it matches the project's issue filter"})
		}
	}
	return map[string]any{"target": target, "skill": SkillFix, "results": outcomes}, nil
}

// QueueCandidates is project_queue_candidates: what target's board can queue
// for skill, with the job that already owns each.
func (m *Manager) QueueCandidates(ctx context.Context, target, repository, skill string) (map[string]any, error) {
	engine := m.engine()
	if engine == nil {
		return nil, ErrNotWatching
	}
	target, err := queueTarget(engine, target)
	if err != nil {
		return nil, err
	}
	return engine.QueueCandidates(ctx, target, repository, skill)
}
