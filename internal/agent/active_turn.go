package agent

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// ActiveTurnTool is a tool call made so far in a running turn.
type ActiveTurnTool struct {
	ID     string         `json:"id,omitempty"`
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Result string         `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
	Done   bool           `json:"done,omitempty"`
}

// ActiveTurnItem is one entry of a running turn, in the order it streamed:
// a stretch of partial text, a media URL, or a tool call.
type ActiveTurnItem struct {
	Text     string          `json:"text,omitempty"`
	MediaURL string          `json:"media_url,omitempty"`
	Tool     *ActiveTurnTool `json:"tool,omitempty"`
}

// ActiveTurn is what a session's running turns have streamed so far. Any
// client that opens the session mid-turn restores its live state from it.
type ActiveTurn struct {
	AgentID    string           `json:"agent_id"`
	SessionID  string           `json:"session_id"`
	Processing bool             `json:"processing"`
	Items      []ActiveTurnItem `json:"items"`
}

type activeRun struct {
	agentID   string
	sessionID string
	key       string
	id        uint64
	items     []ActiveTurnItem
}

type activeTurnRegistry struct {
	// sendMu keeps snapshots reaching the observer in the order they were
	// taken, so a delayed update never lands after the turn's end.
	sendMu    sync.Mutex
	mu        sync.Mutex
	nextID    uint64
	bySession map[string]map[uint64]*activeRun
	pending   map[string]*time.Timer
}

var activeTurns = &activeTurnRegistry{
	bySession: make(map[string]map[uint64]*activeRun),
	pending:   make(map[string]*time.Timer),
}

type activeTurnObserver func(ActiveTurn)

var activeTurnObs atomic.Pointer[activeTurnObserver]

// activeTurnNotifyDelay coalesces bursts of streamed text into one update.
var activeTurnNotifyDelay = 100 * time.Millisecond

// SetActiveTurnObserver registers an optional observer that receives a
// session's active turn whenever it changes, and an empty one when it ends.
func SetActiveTurnObserver(obs func(ActiveTurn)) {
	if obs == nil {
		activeTurnObs.Store(nil)
		return
	}
	o := activeTurnObserver(obs)
	activeTurnObs.Store(&o)
}

// GetActiveTurn returns what the session's running turns have streamed so far.
func GetActiveTurn(agentID, sessionID string) ActiveTurn {
	key := SessionRuntimeKey(agentID, sessionID)
	activeTurns.mu.Lock()
	defer activeTurns.mu.Unlock()
	return activeTurns.snapshotLocked(agentID, sessionID, key)
}

// ActiveTurns returns every session's active turn.
func ActiveTurns() []ActiveTurn {
	activeTurns.mu.Lock()
	defer activeTurns.mu.Unlock()
	out := make([]ActiveTurn, 0, len(activeTurns.bySession))
	for key, runs := range activeTurns.bySession {
		for _, run := range runs {
			out = append(out, activeTurns.snapshotLocked(run.agentID, run.sessionID, key))
			break
		}
	}
	return out
}

// beginActiveTurn starts recording a run's streamed events for its session.
func beginActiveTurn(agentID, sessionID string) *activeRun {
	key := SessionRuntimeKey(agentID, sessionID)
	if key == "" {
		return nil
	}
	activeTurns.mu.Lock()
	activeTurns.nextID++
	run := &activeRun{agentID: agentID, sessionID: sessionID, key: key, id: activeTurns.nextID}
	runs := activeTurns.bySession[key]
	if runs == nil {
		runs = make(map[uint64]*activeRun)
		activeTurns.bySession[key] = runs
	}
	runs[run.id] = run
	activeTurns.mu.Unlock()
	activeTurns.notify(run, true)
	return run
}

// record adds a streamed event to the run.
func (run *activeRun) record(e StreamEvent) {
	if run == nil {
		return
	}
	activeTurns.mu.Lock()
	changed := true
	immediate := false
	switch e.Type {
	case StreamEventText:
		if e.Text == "" {
			changed = false
			break
		}
		if n := len(run.items); n > 0 && run.items[n-1].Tool == nil && run.items[n-1].MediaURL == "" {
			run.items[n-1].Text += e.Text
		} else {
			run.items = append(run.items, ActiveTurnItem{Text: e.Text})
		}
	case StreamEventMedia:
		if e.MediaURL == "" {
			changed = false
			break
		}
		run.items = append(run.items, ActiveTurnItem{MediaURL: e.MediaURL})
		immediate = true
	case StreamEventTool:
		if e.Tool == nil {
			changed = false
			break
		}
		run.upsertTool(e.Tool)
		immediate = true
	default:
		changed = false
	}
	activeTurns.mu.Unlock()
	if changed {
		activeTurns.notify(run, immediate)
	}
}

// upsertTool updates the call sharing the event's ID in place, so a call and
// its result are one entry.
func (run *activeRun) upsertTool(ev *ToolEvent) {
	tool := ActiveTurnTool{ID: ev.ID, Name: ev.Name, Args: ev.Args, Result: ev.Result, Error: ev.Error, Done: ev.Done}
	if ev.ID != "" {
		for i := len(run.items) - 1; i >= 0; i-- {
			cur := run.items[i].Tool
			if cur == nil || cur.ID != ev.ID {
				continue
			}
			// A finished call stays finished if its start arrives again.
			tool.Done = tool.Done || cur.Done
			if tool.Result == "" {
				tool.Result = cur.Result
			}
			if tool.Error == "" {
				tool.Error = cur.Error
			}
			run.items[i].Tool = &tool
			return
		}
	}
	run.items = append(run.items, ActiveTurnItem{Tool: &tool})
}

// end drops the run once its turn finishes.
func (run *activeRun) end() {
	if run == nil {
		return
	}
	activeTurns.mu.Lock()
	if runs := activeTurns.bySession[run.key]; runs != nil {
		delete(runs, run.id)
		if len(runs) == 0 {
			delete(activeTurns.bySession, run.key)
		}
	}
	activeTurns.mu.Unlock()
	activeTurns.notify(run, true)
}

func (reg *activeTurnRegistry) snapshotLocked(agentID, sessionID, key string) ActiveTurn {
	turn := ActiveTurn{AgentID: agentID, SessionID: sessionID, Items: []ActiveTurnItem{}}
	runs := reg.bySession[key]
	if len(runs) == 0 {
		return turn
	}
	ids := make([]uint64, 0, len(runs))
	for id := range runs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	turn.Processing = true
	for _, id := range ids {
		for _, item := range runs[id].items {
			if item.Tool != nil {
				tool := *item.Tool
				item.Tool = &tool
			}
			turn.Items = append(turn.Items, item)
		}
	}
	return turn
}

// notify sends the run's session snapshot to the observer, at once or after
// a short delay that coalesces further changes.
func (reg *activeTurnRegistry) notify(run *activeRun, immediate bool) {
	if activeTurnObs.Load() == nil {
		return
	}
	send := func() {
		reg.sendMu.Lock()
		defer reg.sendMu.Unlock()
		reg.mu.Lock()
		delete(reg.pending, run.key)
		turn := reg.snapshotLocked(run.agentID, run.sessionID, run.key)
		reg.mu.Unlock()
		if obs := activeTurnObs.Load(); obs != nil {
			(*obs)(turn)
		}
	}
	reg.mu.Lock()
	timer := reg.pending[run.key]
	if immediate {
		if timer != nil {
			timer.Stop()
			delete(reg.pending, run.key)
		}
		reg.mu.Unlock()
		send()
		return
	}
	if timer == nil {
		reg.pending[run.key] = time.AfterFunc(activeTurnNotifyDelay, send)
	}
	reg.mu.Unlock()
}
