package agent

import "context"

type sessionContextKey struct{}

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
