package agent

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/cliagent"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// slowToolClient blocks every slow_tool call until release is closed.
type slowToolClient struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (c *slowToolClient) ListTools(context.Context) ([]ToolInfo, error) {
	return []ToolInfo{{Name: "slow_tool", Description: "Takes a while"}}, nil
}

func (c *slowToolClient) CallToolText(ctx context.Context, _ string, _ map[string]any) (string, error) {
	if c.calls.Add(1) == 1 {
		close(c.started)
	}
	select {
	case <-c.release:
		return "filed", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (c *slowToolClient) Close() error { return nil }

// echoCLISession replies to the last line of each turn, calling slow_tool
// first when that line is "slow".
type echoCLISession struct {
	opts  cliagent.Options
	mu    *sync.Mutex
	turns *[]fakeCLITurn
}

func (s *echoCLISession) Send(ctx context.Context, turn cliagent.Turn, onEvent func(cliagent.Event)) (cliagent.Result, error) {
	lines := strings.Split(turn.Text, "\n")
	last := lines[len(lines)-1]
	if last == "slow" {
		sess, err := connectCLIMCP(ctx, s.opts.MCPURL, s.opts.MCPToken)
		if err != nil {
			return cliagent.Result{}, err
		}
		defer sess.Close() //nolint:errcheck
		if _, err := sess.CallTool(ctx, &sdkmcp.CallToolParams{Name: "slow_tool", Arguments: map[string]any{}}); err != nil {
			return cliagent.Result{}, err
		}
	}
	if id := s.opts.NewSessionID; id != "" {
		onEvent(cliagent.Event{Type: cliagent.EventSession, SessionID: id})
	}
	s.mu.Lock()
	*s.turns = append(*s.turns, fakeCLITurn{opts: s.opts, text: turn.Text})
	s.mu.Unlock()
	return cliagent.Result{Text: "reply to " + last}, nil
}

func (s *echoCLISession) Close() error { return nil }

func promptAsync(runner *AgentRunner, sessionID, message string) <-chan error {
	done := make(chan error, 1)
	runner.Prompt(WithSessionID(context.Background(), sessionID), message, func(e StreamEvent) {
		switch e.Type {
		case StreamEventError:
			done <- e.Err
		case StreamEventDone, StreamEventStop:
			done <- nil
		}
	})
	return done
}

func waitDone(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}
}

func TestCLITurn_SecondMessageRunsWhileFirstWaitsOnASlowTool(t *testing.T) {
	setTestDataDir(t)
	client := &slowToolClient{started: make(chan struct{}), release: make(chan struct{})}
	SetToolClientFactory(func(context.Context) (ToolClient, error) { return client, nil })
	t.Cleanup(func() { SetToolClientFactory(nil) })

	var mu sync.Mutex
	var turns []fakeCLITurn
	prev := newCLISession
	newCLISession = func(opts cliagent.Options) (cliagent.Session, error) {
		return &echoCLISession{opts: opts, mu: &mu, turns: &turns}, nil
	}
	t.Cleanup(func() { newCLISession = prev })

	runner := newCLITestRunner(t, "agent_concurrent", nil)
	sess, err := NewSessionManager().GetOrCreateNamed("agent_concurrent", "main")
	require.NoError(t, err)

	slow := promptAsync(runner, sess.ID, "slow")
	select {
	case <-client.started:
	case <-time.After(10 * time.Second):
		t.Fatal("slow tool never started")
	}

	// The second message finishes while the first is still in its tool call.
	waitDone(t, promptAsync(runner, sess.ID, "fast"))
	select {
	case <-slow:
		t.Fatal("the slow turn finished before its tool was released")
	default:
	}
	close(client.release)
	waitDone(t, slow)
	runner.Wait()

	require.Len(t, turns, 2)
	fast, first := turns[0], turns[1]
	assert.True(t, fast.opts.Ephemeral, "the concurrent turn runs in its own CLI session, not the session's resumable one")
	assert.NotEqual(t, first.opts.MCPToken, fast.opts.MCPToken, "each concurrent turn has its own tool scope")
	assert.Contains(t, fast.text, "<prior_conversation>", "the concurrent turn sees the history up to when it started")
	assert.Contains(t, fast.text, "<user> slow")
	assert.False(t, first.opts.Ephemeral)

	// Replies are appended in order of completion, each linked to its prompt.
	msgs, err := store.ReadJSONL[domain.Message](store.SessionPath("agent_concurrent", sess.ID))
	require.NoError(t, err)
	prompts := map[string]string{}
	var replies []string
	for _, m := range msgs {
		switch m.Role {
		case domain.MessageRoleUser:
			prompts[m.Content] = m.ID
		case domain.MessageRoleAssistant:
			replies = append(replies, m.Content)
		}
	}
	assert.Equal(t, []string{"reply to fast", "reply to slow"}, replies)
	assert.True(t, HasMessageResponse("agent_concurrent", sess.ID, prompts["slow"]))
	assert.True(t, HasMessageResponse("agent_concurrent", sess.ID, prompts["fast"]))

	// The session's own CLI conversation hears about the exchange it missed.
	waitDone(t, promptAsync(runner, sess.ID, "later"))
	runner.Wait()
	require.Len(t, turns, 3)
	assert.Contains(t, turns[2].text, "<concurrent_turns>")
	assert.Contains(t, turns[2].text, "User: fast\nAssistant: reply to fast")
	assert.Equal(t, first.opts.NewSessionID, turns[2].opts.NewSessionID, "the later turn reuses the session's own CLI conversation")
}

func TestCallToolOnce_SharesAnIdenticalInFlightCall(t *testing.T) {
	client := &slowToolClient{started: make(chan struct{}), release: make(chan struct{})}
	args := map[string]any{"title": "Up/down arrows"}

	type result struct {
		text   string
		shared bool
		err    error
	}
	first := make(chan result, 1)
	go func() {
		text, shared, err := callToolOnce(context.Background(), client, "a", "s", "issue_create", args)
		first <- result{text, shared, err}
	}()
	<-client.started

	second := make(chan result, 1)
	go func() {
		text, shared, err := callToolOnce(context.Background(), client, "a", "s", "issue_create", map[string]any{"title": "Up/down arrows"})
		second <- result{text, shared, err}
	}()
	// Let the duplicate reach the in-flight call before it is released.
	require.Eventually(t, func() bool {
		inflightToolCalls.mu.Lock()
		defer inflightToolCalls.mu.Unlock()
		for _, call := range inflightToolCalls.calls {
			return call.waiters == 1
		}
		return false
	}, 5*time.Second, time.Millisecond)
	close(client.release)

	r1, r2 := <-first, <-second
	require.NoError(t, r1.err)
	require.NoError(t, r2.err)
	assert.Equal(t, "filed", r1.text)
	assert.False(t, r1.shared)
	assert.Equal(t, "filed", r2.text)
	assert.True(t, r2.shared)
	assert.EqualValues(t, 1, client.calls.Load(), "the identical call ran once")

	// A different session, or a later call, runs on its own.
	_, shared, err := callToolOnce(context.Background(), client, "a", "other", "issue_create", args)
	require.NoError(t, err)
	assert.False(t, shared)
	assert.EqualValues(t, 2, client.calls.Load())
}
