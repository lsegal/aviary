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

type filesystemGrantKey struct{}
type writeGuardKey struct{}

// WithFilesystemGrant grants the tool calls made with ctx read and write
// access to root, in addition to the agent's static filesystem allowlist.
// Relative file tool paths and exec's default cwd resolve against root. The
// grant lasts exactly as long as calls carry ctx, so a job runtime that grants
// its clone directory loses access when the job's context is no longer used.
func WithFilesystemGrant(ctx context.Context, root string) context.Context {
	if root == "" {
		return ctx
	}
	return context.WithValue(ctx, filesystemGrantKey{}, root)
}

// FilesystemGrantFromContext returns the granted root directory if present.
func FilesystemGrantFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(filesystemGrantKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

type projectTaskScriptKey struct{}

// WithProjectTaskScript marks ctx as a precompiled project task's script, or
// the compile and analysis of one. Such a script runs in a fresh clone of its
// project, where the task would otherwise run as a coding agent with a shell,
// so exec is available to it whatever the agent's own exec permissions are,
// confined to the clone its context grants (see WithFilesystemGrant). Listing
// tools with ctx, which is what compiles, analyzes and runs the script, is
// how all three see the same tools.
func WithProjectTaskScript(ctx context.Context) context.Context {
	return context.WithValue(ctx, projectTaskScriptKey{}, true)
}

// IsProjectTaskScript reports whether ctx is a project task's script.
func IsProjectTaskScript(ctx context.Context) bool {
	v, _ := ctx.Value(projectTaskScriptKey{}).(bool)
	return v
}

type commandEnvKey struct{}

// WithCommandEnv sets environment variables for the commands tool calls made
// with ctx start, on top of aviary's own environment and the agent's .env, as
// a precompiled project task's script gets AVIARY_CHECKOUT_DIRECTORY.
func WithCommandEnv(ctx context.Context, env map[string]string) context.Context {
	if len(env) == 0 {
		return ctx
	}
	return context.WithValue(ctx, commandEnvKey{}, env)
}

// CommandEnvFromContext returns the variables WithCommandEnv set, if any.
func CommandEnvFromContext(ctx context.Context) map[string]string {
	env, _ := ctx.Value(commandEnvKey{}).(map[string]string)
	return env
}

// WithWriteGuard marks ctx as a model-driven tool call, where file_write must
// not overwrite an existing file the session has not read.
func WithWriteGuard(ctx context.Context) context.Context {
	return context.WithValue(ctx, writeGuardKey{}, true)
}

// WriteGuardFromContext reports whether file_write's read-before-overwrite
// guard applies to ctx.
func WriteGuardFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(writeGuardKey{}).(bool)
	return v
}
