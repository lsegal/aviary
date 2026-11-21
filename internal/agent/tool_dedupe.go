package agent

import (
	"context"
	"encoding/json"
	"sync"
)

// Turns in one session run concurrently (#714), so two of them can make the
// same tool call at once, such as filing the same issue for a request the user
// sent twice. A call identical to one already running in the session waits
// for that call and shares its result instead of running a second time.

// sharedToolCallNote prefixes the result a duplicate call receives.
const sharedToolCallNote = "Note: an identical call from another request in this session was already running, so this call did not run again. Its result:\n\n"

type inflightToolCall struct {
	done    chan struct{}
	text    string
	err     error
	waiters int // duplicate calls sharing this one; guarded by inflightToolCalls.mu
}

var inflightToolCalls = struct {
	mu    sync.Mutex
	calls map[string]*inflightToolCall
}{calls: map[string]*inflightToolCall{}}

// callToolOnce runs the tool call through client, unless an identical call is
// already running in the session, in which case it waits for that call and
// returns its result. shared reports that the result came from the other call.
func callToolOnce(ctx context.Context, client ToolClient, agentID, sessionID, name string, args map[string]any) (text string, shared bool, err error) {
	if sessionID == "" {
		text, err = client.CallToolText(ctx, name, args)
		return text, false, err
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		text, err = client.CallToolText(ctx, name, args)
		return text, false, err
	}
	key := agentID + "\x00" + sessionID + "\x00" + name + "\x00" + string(encoded)

	inflightToolCalls.mu.Lock()
	if call, ok := inflightToolCalls.calls[key]; ok {
		call.waiters++
		inflightToolCalls.mu.Unlock()
		select {
		case <-call.done:
			return call.text, true, call.err
		case <-ctx.Done():
			return "", false, ctx.Err()
		}
	}
	call := &inflightToolCall{done: make(chan struct{})}
	inflightToolCalls.calls[key] = call
	inflightToolCalls.mu.Unlock()

	call.text, call.err = client.CallToolText(ctx, name, args)
	inflightToolCalls.mu.Lock()
	delete(inflightToolCalls.calls, key)
	inflightToolCalls.mu.Unlock()
	close(call.done)
	return call.text, false, call.err
}
