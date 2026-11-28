package llm

import (
	"fmt"
	"strings"
)

// pendingToolCall accumulates one tool call while a provider streams it.
type pendingToolCall struct {
	ID        string
	Name      string
	Arguments strings.Builder
	// hasArgsDelta records that streamed argument deltas replaced any
	// arguments the call started with.
	hasArgsDelta bool
}

// pendingToolCalls holds streamed tool calls keyed by their output index.
type pendingToolCalls map[int64]*pendingToolCall

// at returns the call at index, creating it if needed.
func (calls pendingToolCalls) at(index int64) *pendingToolCall {
	call := calls[index]
	if call == nil {
		call = &pendingToolCall{}
		calls[index] = call
	}
	return call
}

// applyDelta merges a Chat Completions style tool call delta: non-empty ID
// and name replace the current values and arguments are appended.
func (calls pendingToolCalls) applyDelta(index int64, id, name, arguments string) {
	call := calls.at(index)
	if id != "" {
		call.ID = id
	}
	if name != "" {
		call.Name = name
	}
	if arguments != "" {
		call.Arguments.WriteString(arguments)
	}
}

// appendStreamedArguments appends a streamed argument fragment. The first
// fragment discards any arguments the call was started with.
func (call *pendingToolCall) appendStreamedArguments(fragment string) {
	if !call.hasArgsDelta {
		call.Arguments.Reset()
		call.hasArgsDelta = true
	}
	call.Arguments.WriteString(fragment)
}

// emit sends the named calls to ch in index order. If a call's arguments do
// not parse it sends an error event labeled with label and returns false.
func (calls pendingToolCalls) emit(ch chan<- Event, label string) bool {
	indexes := make([]int, 0, len(calls))
	for idx := range calls {
		indexes = append(indexes, int(idx))
	}
	sortInts(indexes)
	for _, idx := range indexes {
		call := calls[int64(idx)]
		if call == nil || strings.TrimSpace(call.Name) == "" {
			continue
		}
		args, err := parseToolArguments(call.Arguments.String())
		if err != nil {
			ch <- Event{Type: EventTypeError, Error: fmt.Errorf("%s tool arguments for %q: %w", label, call.Name, err)}
			return false
		}
		ch <- Event{Type: EventTypeToolCall, ToolCall: &ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: args,
		}}
	}
	return true
}

// appendLine joins line onto text with a newline, skipping empty lines.
func appendLine(text, line string) string {
	if line == "" {
		return text
	}
	if text == "" {
		return line
	}
	return text + "\n" + line
}
