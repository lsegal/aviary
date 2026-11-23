package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/store"
)

// UsageSourceTaskDelivery marks a usage record that logs where a task's or
// workflow's output was delivered rather than tokens spent.
const UsageSourceTaskDelivery = "task_delivery"

// deliverOutput sends a task's or workflow's output to route, resolved by
// config.ParseTaskTarget, the resolver tasks and workflows share, and reports
// the outcome for the run log. Every delivery of a non-silent target is also
// written to the usage log.
func (s *Server) deliverOutput(source domain.OutputSource, agentName, route, text string) (*domain.JobDelivery, error) {
	target, err := config.ParseTaskTarget(route)
	if err == nil && (target.Kind == config.TargetSilent || target.Kind == config.TargetReply) {
		// A reply is the workflow hooks' to deliver.
		return nil, nil
	}
	delivery := &domain.JobDelivery{Target: strings.TrimSpace(route), At: time.Now().UTC()}
	if err == nil {
		switch target.Kind {
		case config.TargetSession:
			err = deliverToSession(agentName, target.Session, text)
			delivery.Message = "delivered to session " + target.Session
		case config.TargetChannel:
			err = s.deliverToChannel(agentName, target.Channel, text)
			delivery.Message = "delivered to channel " + target.Channel
		case config.TargetCodeLoop:
			err = s.deliverToCodeLoop(source, target, text, delivery)
		}
	}
	if err != nil {
		delivery.Status, delivery.Message = domain.DeliveryFailed, err.Error()
	} else if delivery.Status == "" {
		delivery.Status = domain.DeliveryDelivered
	}
	s.recordDelivery(source, agentName, delivery)
	return delivery, err
}

// deliverToSession appends text to agentName's session named or identified by
// ref, creating a named session on first delivery.
func deliverToSession(agentName, ref, text string) error {
	if store.FindSessionPath(agentName, ref) != "" {
		return agent.AppendReplyToSession(agentName, ref, text)
	}
	sess, err := agent.NewSessionManager().GetOrCreateNamed(agentName, ref)
	if err != nil {
		return fmt.Errorf("resolving task target session %q: %w", ref, err)
	}
	return agent.AppendReplyToSession(agentName, sess.ID, text)
}

// deliverToCodeLoop hands text to the Code Loop job target names, the way an
// allowed commenter's comment reaches it, and fills in delivery.
func (s *Server) deliverToCodeLoop(source domain.OutputSource, target config.TaskTarget, text string, delivery *domain.JobDelivery) error {
	ref := target.Project + "#" + fmt.Sprint(target.Number)
	delivery.Project, delivery.Number = target.Project, target.Number
	delivery.Owner = s.projectOwner(target.Project)
	if s.projects == nil {
		return errors.New("the Code Loop engine is not running, so the output could not reach " + ref)
	}
	label := source.Label()
	if source.Task == "" {
		label = "Aviary"
	}
	outcome, _, err := s.projects.SendJobMessage(context.Background(), target.Project, target.Number, projects.InboxMessage{
		Source:    label,
		SourceURL: taskRunPath(source),
		Text:      text,
	}, projects.JobDeliveryOptions{Resume: target.Resume, Reopen: target.Reopen})
	switch {
	case errors.Is(err, projects.ErrJobGone):
		return fmt.Errorf("the Code Loop job %s no longer exists", ref)
	case err != nil:
		return err
	case outcome == projects.InboxQueued:
		delivery.Status = domain.DeliveryQueued
		delivery.Message = "queued for Code Loop " + ref + " until the job runs again"
	case outcome == projects.InboxResumed:
		delivery.Status = domain.DeliveryDelivered
		delivery.Message = "resumed Code Loop " + ref + " with the output"
	case outcome == projects.InboxReopened:
		delivery.Status = domain.DeliveryDelivered
		delivery.Message = "reopened Code Loop " + ref + " with the output"
	default:
		delivery.Status = domain.DeliveryDelivered
		delivery.Message = "delivered to Code Loop " + ref
	}
	return nil
}

// projectOwner names the agent that owns project, or "" for a runtime watch.
func (s *Server) projectOwner(project string) string {
	if s.cfg == nil {
		return ""
	}
	for _, a := range s.cfg.Agents {
		for _, p := range a.Projects {
			if strings.EqualFold(strings.TrimSpace(p.Target), project) {
				return a.Name
			}
		}
	}
	return ""
}

// taskRunPath is the web UI path of source's run, or "".
func taskRunPath(source domain.OutputSource) string {
	agentName, task, ok := strings.Cut(source.Task, "/")
	if !ok || source.JobID == "" {
		return ""
	}
	return "/agents/" + url.PathEscape(agentName) + "/tasks/" + url.PathEscape(task) + "/runs/" + url.PathEscape(source.JobID)
}

// recordDelivery logs delivery and writes it to the usage log.
func (s *Server) recordDelivery(source domain.OutputSource, agentName string, delivery *domain.JobDelivery) {
	slog.Info("task output delivery", "task", source.Task, "job", source.JobID, "target", delivery.Target, "status", delivery.Status, "message", delivery.Message)
	rec := domain.UsageRecord{
		Timestamp: delivery.At,
		AgentID:   agentName,
		Source:    UsageSourceTaskDelivery,
		Task:      source.Task,
		HasError:  delivery.Status == domain.DeliveryFailed,
		Delivery:  delivery,
	}
	if err := store.AppendJSONL(store.UsagePath(), rec); err != nil {
		slog.Warn("server: failed to record task output delivery", "err", err)
	}
}
