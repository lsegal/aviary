package server

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/store"
)

// workflowContextMessages is how many messages before and after a workflow's
// triggering message its run records.
const workflowContextMessages = 3

// handleWorkflowMessage hands msg to the agent's active workflows that match
// it. It reports whether any did, in which case the workflows replace the
// agent's chat reply: the message is recorded in sessionID, marked with the
// workflows and runs that handled it, and the agent does not answer it.
func (s *Server) handleWorkflowMessage(agentName string, cc config.ChannelConfig, sessionID string, msg channels.IncomingMessage) bool {
	if s.sched == nil || fromAgent(cc, msg) {
		return false
	}
	if len(s.sched.MatchWorkflows(agentName, cc, msg.Channel, msg.Text)) == 0 {
		return false
	}
	trigger := domain.TriggerContext{
		ChannelType:  cc.Type,
		ConfiguredID: cc.ID,
		Channel:      cc.EffectiveName(),
		Entry:        msg.Channel,
		ThreadTS:     strings.TrimSpace(msg.ThreadTS),
		SessionID:    sessionID,
		RecordedAt:   time.Now(),
		Message: domain.TriggerMessage{
			Sender:      scheduler.DisplayName(msg.From, msg.SenderName),
			Text:        msg.Text,
			Attachments: attachmentLabels(msg.Attachments),
			Timestamp:   messageTime(msg.ReceivedAt),
		},
	}
	if sessionID != "" {
		trigger.Before = sessionMessagesAround(agentName, sessionID, trigger.RecordedAt, true)
	}
	fires := s.sched.FireWorkflows(agentName, cc, trigger)
	if len(fires) == 0 {
		// The workflows were paused or removed since they matched.
		return false
	}
	handled := make([]domain.MessageWorkflow, 0, len(fires))
	names := make([]string, 0, len(fires))
	for _, f := range fires {
		handled = append(handled, domain.MessageWorkflow{TaskID: f.TaskID, Name: f.Name, JobID: f.JobID})
		names = append(names, f.Name)
	}
	slog.Info("server: channel message handled by workflows", "agent", agentName, "channel", cc.EffectiveName(), "workflows", strings.Join(names, ","))
	if sessionID != "" {
		text, media := channels.PromptInput(msg.Text, msg.Attachments)
		sender := domain.NewMessageSender(msg.From, msg.SenderName, true)
		if err := agent.AppendWorkflowMessageToSession(agentName, sessionID, text, media, sender, handled, trigger.RecordedAt); err != nil {
			slog.Warn("server: failed to record workflow message", "session", sessionID, "err", err)
		}
	}
	return true
}

// fromAgent reports whether msg was sent by the agent itself on cc, such as
// a Signal message from the channel's own number. Workflows never fire on
// those, so a workflow cannot loop on its own replies.
func fromAgent(cc config.ChannelConfig, msg channels.IncomingMessage) bool {
	from := strings.TrimSpace(msg.From)
	return from != "" && strings.EqualFold(from, strings.TrimSpace(cc.ID))
}

// workflowReplySender is the part of *channels.Manager that delivers workflow
// replies, so tests can substitute a channel without a real platform.
type workflowReplySender interface {
	SendOnConfiguredChannel(agentName, channelType, configuredID, channelID, text string) error
	SendThreadOnConfiguredChannel(agentName, channelType, configuredID, channelID, threadTS, text string) error
}

// replyToWorkflowTrigger sends a workflow run's output to the conversation
// whose message triggered it, through that conversation's channel session so
// the reply also shows in its chat.
func (s *Server) replyToWorkflowTrigger(agentName string, tc domain.TriggerContext, text string) error {
	if tc.SessionID != "" && store.FindSessionPath(agentName, tc.SessionID) != "" {
		return agent.AppendReplyToSession(agentName, tc.SessionID, text)
	}
	if tc.ThreadTS != "" {
		return s.workflowChannels.SendThreadOnConfiguredChannel(agentName, tc.ChannelType, tc.ConfiguredID, tc.Entry, tc.ThreadTS, text)
	}
	return s.workflowChannels.SendOnConfiguredChannel(agentName, tc.ChannelType, tc.ConfiguredID, tc.Entry, text)
}

// workflowMessagesAfter returns the messages that followed a workflow run's
// triggering message in its conversation.
func workflowMessagesAfter(agentName string, tc domain.TriggerContext) []domain.TriggerMessage {
	if tc.SessionID == "" {
		return nil
	}
	return sessionMessagesAround(agentName, tc.SessionID, tc.RecordedAt, false)
}

// sessionMessagesAround returns up to workflowContextMessages user and
// assistant messages of the session recorded before at, or after it.
func sessionMessagesAround(agentName, sessionID string, at time.Time, before bool) []domain.TriggerMessage {
	path := store.FindSessionPath(agentName, sessionID)
	if path == "" {
		return nil
	}
	lines, err := store.ReadJSONL[domain.Message](path)
	if err != nil {
		return nil
	}
	var out []domain.TriggerMessage
	for _, m := range lines {
		if m.Role != domain.MessageRoleUser && m.Role != domain.MessageRoleAssistant {
			continue
		}
		if (before && !m.Timestamp.Before(at)) || (!before && !m.Timestamp.After(at)) {
			continue
		}
		tm := domain.TriggerMessage{
			FromAgent: m.Role == domain.MessageRoleAssistant,
			Text:      m.Content,
			Timestamp: m.Timestamp,
		}
		if m.Sender != nil && !tm.FromAgent {
			tm.Sender = scheduler.DisplayName(m.Sender.ID, m.Sender.Name)
		}
		out = append(out, tm)
	}
	if before && len(out) > workflowContextMessages {
		out = out[len(out)-workflowContextMessages:]
	}
	if !before && len(out) > workflowContextMessages {
		out = out[:workflowContextMessages]
	}
	return out
}

// attachmentLabels describes a message's attachments for its trigger context.
func attachmentLabels(atts []channels.Attachment) []string {
	out := make([]string, 0, len(atts))
	for _, a := range atts {
		label := a.Name
		if label == "" {
			label = a.ContentType
		}
		if a.Path != "" {
			label = fmt.Sprintf("%s (%s)", label, a.Path)
		}
		out = append(out, label)
	}
	return out
}

func messageTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t
}
