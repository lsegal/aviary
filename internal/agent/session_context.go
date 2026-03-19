package agent

import (
	"context"

	"github.com/lsegal/aviary/internal/domain"
)

type sessionContextKey struct{}
type sessionAgentIDKey struct{}
type channelSessionKey struct{}
type sessionSenderKey struct{}
type taskIDKey struct{}
type jobIDKey struct{}

type channelSession struct {
	channelType  string
	configuredID string
	channelID    string
}

// WithSessionID stores a concrete session ID in context for prompt execution.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionContextKey{}, sessionID)
}

// SessionIDFromContext extracts the session ID if present.
func SessionIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(sessionContextKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// WithSessionAgentID stores the agent ID that owns the current session in context.
func WithSessionAgentID(ctx context.Context, agentID string) context.Context {
	if agentID == "" {
		return ctx
	}
	return context.WithValue(ctx, sessionAgentIDKey{}, agentID)
}

// SessionAgentIDFromContext extracts the session's agent ID if present.
func SessionAgentIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(sessionAgentIDKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// WithChannelSession stores the originating configured channel and target ID in
// context so that prompt execution and scheduled tasks can route replies back
// to the exact source channel by default.
func WithChannelSession(ctx context.Context, channelType, configuredID, channelID string) context.Context {
	if channelType == "" || channelID == "" {
		return ctx
	}
	return context.WithValue(ctx, channelSessionKey{}, channelSession{
		channelType:  channelType,
		configuredID: configuredID,
		channelID:    channelID,
	})
}

// ChannelSessionFromContext extracts the channel session info if present.
func ChannelSessionFromContext(ctx context.Context) (channelType, configuredID, channelID string, ok bool) {
	v, ok := ctx.Value(channelSessionKey{}).(channelSession)
	if !ok || v.channelType == "" {
		return "", "", "", false
	}
	return v.channelType, v.configuredID, v.channelID, true
}

// WithSessionSender stores structured sender information for the current user turn.
func WithSessionSender(ctx context.Context, sender *domain.MessageSender) context.Context {
	if sender == nil {
		return ctx
	}
	senderValue := *sender
	return context.WithValue(ctx, sessionSenderKey{}, senderValue)
}

// SessionSenderFromContext extracts the structured sender for the current user turn.
func SessionSenderFromContext(ctx context.Context) (*domain.MessageSender, bool) {
	v, ok := ctx.Value(sessionSenderKey{}).(domain.MessageSender)
	if !ok {
		return nil, false
	}
	senderValue := v
	return &senderValue, true
}

// WithTaskID stores the current scheduled task ID in context.
func WithTaskID(ctx context.Context, taskID string) context.Context {
	if taskID == "" {
		return ctx
	}
	return context.WithValue(ctx, taskIDKey{}, taskID)
}

// TaskIDFromContext extracts the current scheduled task ID if present.
func TaskIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(taskIDKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// WithJobID stores the current scheduled job ID in context.
func WithJobID(ctx context.Context, jobID string) context.Context {
	if jobID == "" {
		return ctx
	}
	return context.WithValue(ctx, jobIDKey{}, jobID)
}

// JobIDFromContext extracts the current scheduled job ID if present.
func JobIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(jobIDKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
