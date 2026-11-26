package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/domain"
)

// TestStringContextValues round-trips each string-valued With*/From* pair,
// reads it from a bare context, and checks that an empty value leaves the
// context unchanged.
func TestStringContextValues(t *testing.T) {
	tests := []struct {
		name string
		with func(context.Context, string) context.Context
		from func(context.Context) (string, bool)
	}{
		{"session ID", WithSessionID, SessionIDFromContext},
		{"session agent ID", WithSessionAgentID, SessionAgentIDFromContext},
		{"task ID", WithTaskID, TaskIDFromContext},
		{"job ID", WithJobID, JobIDFromContext},
		{"filesystem grant", WithFilesystemGrant, FilesystemGrantFromContext},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := context.Background()

			got, ok := tt.from(base)
			assert.False(t, ok)
			assert.Empty(t, got)

			assert.Equal(t, base, tt.with(base, ""), "empty value must return ctx unchanged")

			ctx := tt.with(base, "value-1")
			got, ok = tt.from(ctx)
			assert.True(t, ok)
			assert.Equal(t, "value-1", got)

			ctx = tt.with(ctx, "value-2")
			got, ok = tt.from(ctx)
			assert.True(t, ok)
			assert.Equal(t, "value-2", got, "inner value overrides outer")

			got, ok = tt.from(tt.with(ctx, ""))
			assert.True(t, ok)
			assert.Equal(t, "value-2", got, "empty value keeps the existing one")
		})
	}
}

func TestChannelSessionContext(t *testing.T) {
	base := context.Background()

	typ, cfg, id, ok := ChannelSessionFromContext(base)
	assert.False(t, ok)
	assert.Empty(t, typ+cfg+id)

	assert.Equal(t, base, WithChannelSession(base, "", "cfg", "chan"))
	assert.Equal(t, base, WithChannelSession(base, "slack", "cfg", ""))

	ctx := WithChannelSession(base, "slack", "", "C123")
	typ, cfg, id, ok = ChannelSessionFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "slack", typ)
	assert.Empty(t, cfg, "configured ID is optional")
	assert.Equal(t, "C123", id)

	ctx = WithChannelSession(ctx, "discord", "work", "D456")
	typ, cfg, id, ok = ChannelSessionFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, []string{"discord", "work", "D456"}, []string{typ, cfg, id})
}

func TestSessionSenderContext(t *testing.T) {
	base := context.Background()

	got, ok := SessionSenderFromContext(base)
	assert.False(t, ok)
	assert.Nil(t, got)

	assert.Equal(t, base, WithSessionSender(base, nil))

	sender := &domain.MessageSender{ID: "u1", Name: "Ada", Participant: true}
	ctx := WithSessionSender(base, sender)

	// Mutating the caller's sender after storing it must not change the context.
	sender.Name = "changed"

	got, ok = SessionSenderFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, domain.MessageSender{ID: "u1", Name: "Ada", Participant: true}, *got)

	// Each read returns an independent copy.
	got.Name = "mutated"
	again, ok := SessionSenderFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, "Ada", again.Name)

	// A zero-value sender is still stored and reported as present.
	got, ok = SessionSenderFromContext(WithSessionSender(base, &domain.MessageSender{}))
	require.True(t, ok)
	assert.Equal(t, domain.MessageSender{}, *got)
}

func TestUsageChannelContext(t *testing.T) {
	base := context.Background()

	assert.Empty(t, UsageChannelFromContext(base))
	assert.Equal(t, base, WithUsageChannel(base, ""))

	ctx := WithUsageChannel(base, "slack")
	assert.Equal(t, "slack", UsageChannelFromContext(ctx))
	assert.Equal(t, "slack", UsageChannelFromContext(WithUsageChannel(ctx, "")))
	assert.Equal(t, "web", UsageChannelFromContext(WithUsageChannel(ctx, "web")))
}

func TestBoolContextMarkers(t *testing.T) {
	tests := []struct {
		name string
		with func(context.Context) context.Context
		is   func(context.Context) bool
	}{
		{"project task script", WithProjectTaskScript, IsProjectTaskScript},
		{"write guard", WithWriteGuard, WriteGuardFromContext},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := context.Background()
			assert.False(t, tt.is(base))

			ctx := tt.with(base)
			assert.True(t, tt.is(ctx))
			assert.True(t, tt.is(WithJobID(ctx, "job")), "marker survives derived contexts")
			assert.False(t, tt.is(base), "parent context is not marked")
		})
	}
}

func TestCommandEnvContext(t *testing.T) {
	base := context.Background()

	assert.Nil(t, CommandEnvFromContext(base))
	assert.Equal(t, base, WithCommandEnv(base, nil))
	assert.Equal(t, base, WithCommandEnv(base, map[string]string{}))

	env := map[string]string{"AVIARY_CHECKOUT_DIRECTORY": "/tmp/clone", "FOO": "bar"}
	ctx := WithCommandEnv(base, env)
	assert.Equal(t, map[string]string{"AVIARY_CHECKOUT_DIRECTORY": "/tmp/clone", "FOO": "bar"}, CommandEnvFromContext(ctx))

	// An empty map leaves the existing environment in place.
	assert.Equal(t, env, CommandEnvFromContext(WithCommandEnv(ctx, map[string]string{})))

	// A later call replaces the environment rather than merging it.
	assert.Equal(t, map[string]string{"BAZ": "qux"}, CommandEnvFromContext(WithCommandEnv(ctx, map[string]string{"BAZ": "qux"})))
}

func TestContextValuesAreIndependent(t *testing.T) {
	ctx := context.Background()
	ctx = WithSessionID(ctx, "sess")
	ctx = WithSessionAgentID(ctx, "agent")
	ctx = WithTaskID(ctx, "task")
	ctx = WithJobID(ctx, "job")
	ctx = WithFilesystemGrant(ctx, "/root")
	ctx = WithUsageChannel(ctx, "slack")

	for name, from := range map[string]func(context.Context) (string, bool){
		"sess":  SessionIDFromContext,
		"agent": SessionAgentIDFromContext,
		"task":  TaskIDFromContext,
		"job":   JobIDFromContext,
		"/root": FilesystemGrantFromContext,
	} {
		got, ok := from(ctx)
		assert.True(t, ok)
		assert.Equal(t, name, got)
	}
	assert.Equal(t, "slack", UsageChannelFromContext(ctx))
	assert.False(t, IsProjectTaskScript(ctx))
	assert.False(t, WriteGuardFromContext(ctx))
	assert.Nil(t, CommandEnvFromContext(ctx))
	_, ok := SessionSenderFromContext(ctx)
	assert.False(t, ok)
}
