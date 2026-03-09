package agent

import "context"

type sessionContextKey struct{}
type sessionAgentIDKey struct{}
type channelSessionKey struct{}

type channelSession struct {
	channelType string
	channelID   string
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

// WithChannelSession stores the originating channel type and ID in context so
// that resolveSessionID can route the prompt to a per-channel session.
func WithChannelSession(ctx context.Context, channelType, channelID string) context.Context {
	if channelType == "" || channelID == "" {
		return ctx
	}
	return context.WithValue(ctx, channelSessionKey{}, channelSession{channelType, channelID})
}

// ChannelSessionFromContext extracts the channel session info if present.
func ChannelSessionFromContext(ctx context.Context) (channelType, channelID string, ok bool) {
	v, ok := ctx.Value(channelSessionKey{}).(channelSession)
	if !ok || v.channelType == "" {
		return "", "", false
	}
	return v.channelType, v.channelID, true
}
