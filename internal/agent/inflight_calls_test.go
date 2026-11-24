package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/cliagent"
)

// refilingCLISession files an issue through slow_tool for a turn ending in
// "file it", and files it again for any later turn that is not told the
// first filing is still running, the way a model takes the unanswered
// request in its history for its own.
type refilingCLISession struct {
	opts  cliagent.Options
	mu    *sync.Mutex
	turns *[]fakeCLITurn
}

func (s *refilingCLISession) Send(ctx context.Context, turn cliagent.Turn, _ func(cliagent.Event)) (cliagent.Result, error) {
	s.mu.Lock()
	*s.turns = append(*s.turns, fakeCLITurn{opts: s.opts, text: turn.Text})
	s.mu.Unlock()
	if strings.HasSuffix(turn.Text, "file it") || !strings.Contains(turn.Text, "<in_flight_tool_calls>") {
		sess, err := connectCLIMCP(ctx, s.opts.MCPURL, s.opts.MCPToken)
		if err != nil {
			return cliagent.Result{}, err
		}
		defer sess.Close() //nolint:errcheck
		args := map[string]any{"text": "Browser profile lock false positive"}
		if _, err := sess.CallTool(ctx, &sdkmcp.CallToolParams{Name: "slow_tool", Arguments: args}); err != nil {
			return cliagent.Result{}, err
		}
	}
	return cliagent.Result{Text: "done"}, nil
}

func (s *refilingCLISession) Close() error { return nil }

// A message sent while an earlier turn's tool call is still running is told
// about that call, so it does not handle the earlier request again (#1174).
func TestCLITurn_ConcurrentTurnSeesTheInFlightToolCall(t *testing.T) {
	setTestDataDir(t)
	client := &slowToolClient{started: make(chan struct{}), release: make(chan struct{})}
	SetToolClientFactory(func(context.Context) (ToolClient, error) { return client, nil })
	t.Cleanup(func() { SetToolClientFactory(nil) })

	var mu sync.Mutex
	var turns []fakeCLITurn
	prev := newCLISession
	newCLISession = func(opts cliagent.Options) (cliagent.Session, error) {
		return &refilingCLISession{opts: opts, mu: &mu, turns: &turns}, nil
	}
	t.Cleanup(func() { newCLISession = prev })

	runner := newCLITestRunner(t, "agent_inflight", nil)
	sess, err := NewSessionManager().GetOrCreateNamed("agent_inflight", "main")
	require.NoError(t, err)

	first := promptAsync(runner, sess.ID, "please file it")
	select {
	case <-client.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the filing never started")
	}
	waitDone(t, promptAsync(runner, sess.ID, "why can't you run gh?"))
	close(client.release)
	waitDone(t, first)
	runner.Wait()

	assert.EqualValues(t, 1, client.calls.Load(), "the second turn does not file the issue again")
	require.Len(t, turns, 2)
	second := turns[1].text
	assert.Contains(t, second, "<in_flight_tool_calls>")
	assert.Contains(t, second, `slow_tool {"text":"Browser profile lock false positive"} — still running`)
	assert.Less(t, strings.Index(second, "</in_flight_tool_calls>"), strings.Index(second, "why can't you run gh?"), "the note comes before the new message")

	// Once the call returns, later turns are not told about it.
	assert.Empty(t, runningSessionToolCalls("agent_inflight", sess.ID))
}

func TestInFlightToolCallsBlock(t *testing.T) {
	assert.Empty(t, inFlightToolCallsBlock(nil, time.Now()))

	now := time.Now()
	untrackOld := trackSessionToolCall("a", "s", sessionToolCall{id: "1", name: "project_issue_create", args: map[string]any{"text": strings.Repeat("é", maxListedToolText)}, started: now.Add(-58 * time.Second)})
	untrackNew := trackSessionToolCall("a", "s", sessionToolCall{id: "2", name: "browser_open", started: now.Add(-time.Second)})
	defer untrackNew()
	trackSessionToolCall("a", "", sessionToolCall{id: "3", name: "untracked"})()

	calls := runningSessionToolCalls("a", "s")
	require.Len(t, calls, 2)
	assert.Equal(t, []string{"project_issue_create", "browser_open"}, []string{calls[0].name, calls[1].name}, "oldest first")
	assert.Empty(t, runningSessionToolCalls("a", "other"))

	block := inFlightToolCallsBlock(calls, now)
	assert.Contains(t, block, `- project_issue_create {"text":"éé`)
	assert.Contains(t, block, "éé… — still running, started 58s ago", "long arguments are cut at a rune")
	assert.Contains(t, block, "- browser_open — still running, started 1s ago")

	untrackOld()
	require.Len(t, runningSessionToolCalls("a", "s"), 1)
}
