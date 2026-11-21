package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

// setupTitleTest gives an agent "assistant" a fresh data dir and replaces the
// title generator with gen, returning an in-process client.
func setupTitleTest(t *testing.T, gen func(context.Context, string, string) (string, error)) *InProcessClient {
	t.Helper()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	require.NoError(t, store.EnsureDirs())

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "assistant", Model: "stub"}}})
	SetDeps(&Deps{Agents: mgr})

	prevGen := sessionTitleGenerator
	t.Cleanup(func() { sessionTitleGenerator = prevGen })
	sessionTitleGenerator = gen

	c, err := NewInProcessClient(context.Background(), NewServer())
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() }) //nolint:errcheck
	return c
}

func runPrompt(t *testing.T, c *InProcessClient, args map[string]any) {
	t.Helper()
	_, err := c.CallTool(context.Background(), "agent_run", args)
	require.NoError(t, err)
}

func TestAgentRun_TitlesNewChatFromFirstPrompt(t *testing.T) {
	var calls atomic.Int32
	var gotPrompt atomic.Value
	c := setupTitleTest(t, func(_ context.Context, agentName, prompt string) (string, error) {
		calls.Add(1)
		gotPrompt.Store(agentName + "|" + prompt)
		return "Title: \"Weekend trip plans.\"", nil
	})
	sess, err := agent.NewSessionManager().Create("assistant")
	require.NoError(t, err)

	runPrompt(t, c, map[string]any{"name": "assistant", "message": "help me plan a weekend trip", "session_id": sess.ID})
	require.Eventually(t, func() bool {
		return agent.SessionTitle("assistant", sess.ID) == "Weekend trip plans"
	}, 5*time.Second, 10*time.Millisecond)
	assert.Equal(t, "assistant|help me plan a weekend trip", gotPrompt.Load())

	// Later prompts keep the title.
	runPrompt(t, c, map[string]any{"name": "assistant", "message": "something else", "session_id": sess.ID})
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, "Weekend trip plans", agent.SessionTitle("assistant", sess.ID))
}

func TestAgentRun_LeavesChatUnnamedUntilTheModelAnswers(t *testing.T) {
	var attempt atomic.Int32
	c := setupTitleTest(t, func(context.Context, string, string) (string, error) {
		switch attempt.Add(1) {
		case 1:
			return "", errors.New("no provider")
		case 2:
			return "  \n", nil
		}
		return "Sky color physics", nil
	})
	sess, err := agent.NewSessionManager().Create("assistant")
	require.NoError(t, err)

	runPrompt(t, c, map[string]any{"name": "assistant", "message": "why is the sky blue?", "session_id": sess.ID})
	require.Eventually(t, func() bool { return attempt.Load() >= 1 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, agent.SessionTitle("assistant", sess.ID))

	// An empty reply leaves it unnamed too.
	runPrompt(t, c, map[string]any{"name": "assistant", "message": "why is the sky blue?", "session_id": sess.ID})
	require.Eventually(t, func() bool { return attempt.Load() >= 2 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, agent.SessionTitle("assistant", sess.ID))

	runPrompt(t, c, map[string]any{"name": "assistant", "message": "and at sunset?", "session_id": sess.ID})
	require.Eventually(t, func() bool {
		return agent.SessionTitle("assistant", sess.ID) == "Sky color physics"
	}, 5*time.Second, 10*time.Millisecond)
}

func TestAgentRun_TitlesUntitledChatOnLaterPrompt(t *testing.T) {
	c := setupTitleTest(t, func(_ context.Context, _, prompt string) (string, error) {
		return "About " + prompt, nil
	})
	sess, err := agent.NewSessionManager().Create("assistant")
	require.NoError(t, err)
	// The chat's first exchange happened without naming it.
	require.NoError(t, agent.AppendMessageToSession("assistant", sess.ID, "user", "test"))
	require.NoError(t, agent.AppendMessageToSession("assistant", sess.ID, "assistant", "hello"))

	runPrompt(t, c, map[string]any{"name": "assistant", "message": "gardening", "session_id": sess.ID})
	require.Eventually(t, func() bool {
		return agent.SessionTitle("assistant", sess.ID) == "About gardening"
	}, 5*time.Second, 10*time.Millisecond)
}

func TestAgentRun_ImageOnlyFirstPromptLeavesChatForNextPrompt(t *testing.T) {
	c := setupTitleTest(t, func(context.Context, string, string) (string, error) {
		return "Testing", nil
	})
	sess, err := agent.NewSessionManager().Create("assistant")
	require.NoError(t, err)

	runPrompt(t, c, map[string]any{"name": "assistant", "message": "   ", "session_id": sess.ID})
	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, agent.SessionTitle("assistant", sess.ID))

	runPrompt(t, c, map[string]any{"name": "assistant", "message": "test", "session_id": sess.ID})
	require.Eventually(t, func() bool {
		return agent.SessionTitle("assistant", sess.ID) == "Testing"
	}, 5*time.Second, 10*time.Millisecond)
}

func TestAgentRun_LeavesMainAndNamedSessionsUntitled(t *testing.T) {
	var calls atomic.Int32
	c := setupTitleTest(t, func(context.Context, string, string) (string, error) {
		calls.Add(1)
		return "Should not be used", nil
	})
	named, err := agent.NewSessionManager().CreateWithName("assistant", "signal:+15551234567")
	require.NoError(t, err)

	runPrompt(t, c, map[string]any{"name": "assistant", "message": "hello main"})
	runPrompt(t, c, map[string]any{"name": "assistant", "message": "hello channel", "session_id": named.ID})
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), calls.Load())
	assert.Empty(t, agent.SessionTitle("assistant", "main"))
	assert.Empty(t, agent.SessionTitle("assistant", named.ID))
}

func TestSessionRename_ListsTitleAndLastMessageTime(t *testing.T) {
	c := setupTitleTest(t, func(context.Context, string, string) (string, error) { return "", nil })
	sess, err := agent.NewSessionManager().Create("assistant")
	require.NoError(t, err)
	require.NoError(t, agent.AppendMessageToSession("assistant", sess.ID, "user", "hi"))

	res, err := c.CallTool(context.Background(), "session_rename", map[string]any{
		"agent": "assistant", "session_id": sess.ID, "name": "  Groceries  ",
	})
	require.NoError(t, err)
	assert.False(t, res.IsError, extractText(res))

	res, err = c.CallTool(context.Background(), "session_list", map[string]any{"agent": "assistant"})
	require.NoError(t, err)
	var listed []struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Title         string `json:"title"`
		LastMessageAt string `json:"last_message_at"`
	}
	require.NoError(t, json.Unmarshal([]byte(extractText(res)), &listed))
	require.Len(t, listed, 2)
	assert.Equal(t, "main", listed[0].Name)
	assert.Empty(t, listed[0].LastMessageAt)
	assert.Equal(t, sess.ID, listed[1].ID)
	assert.Equal(t, sess.ID, listed[1].Name)
	assert.Equal(t, "Groceries", listed[1].Title)
	assert.NotEmpty(t, listed[1].LastMessageAt)

	res, err = c.CallTool(context.Background(), "session_rename", map[string]any{
		"agent": "assistant", "session_id": sess.ID, "name": " ",
	})
	require.NoError(t, err)
	assert.True(t, res.IsError)

	res, err = c.CallTool(context.Background(), "session_remove", map[string]any{"agent": "assistant", "session_id": sess.ID})
	require.NoError(t, err)
	assert.False(t, res.IsError, extractText(res))
	_, statErr := os.Stat(store.SessionMetaPath("assistant", sess.ID))
	assert.True(t, os.IsNotExist(statErr))
}

func TestCleanSessionTitle(t *testing.T) {
	cases := map[string]string{
		"Weekend trip plans":                 "Weekend trip plans",
		"\n\n**Debugging Go builds**\nextra": "Debugging Go builds",
		"title: 'Fixing CI.'":                "Fixing CI",
		"“Quoted”":                           "Quoted",
		"":                                   "",
		strings.Repeat("word ", 20):          "word word word word word word word word word word word…",
		"time: 10:30 meeting":                "time: 10:30 meeting",
	}
	for in, want := range cases {
		assert.Equal(t, want, cleanSessionTitle(in), "input %q", in)
	}
}
