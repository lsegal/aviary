package clidefs

import "fmt"

// Wakeup describes how an agent's event stream signals a deferred turn: a
// tool call that ends the turn asking to be woken later, as Claude Code's
// ScheduleWakeup does. A CLI run in print mode exits as soon as its turn ends,
// so the wakeup never fires on its own; aviary reads the call out of the
// stream, waits out its delay, and resumes the session with its prompt.
//
// Every field is a path in the same dot-separated, []-stepping syntax the
// JSONL decoder reads. Calls is walked from the event, Name, ID, and Input
// from each call it names, and Delay, Prompt, Reason, and Stop from the input.
// The stream is read for wakeups whatever output format decodes it, so the
// signal is declared once, here, rather than in each decoder.
type Wakeup struct {
	// Type is the path to the event's type, read only to match Event and
	// Rejected.Event. An empty path matches every event.
	Type string `json:"type,omitempty"`
	// Event is the type of the events that carry the call. It needs Type;
	// empty reads calls out of every event.
	Event string `json:"event,omitempty"`
	// Calls is the path to each call an event carries, such as
	// "message.content[]". Empty reads the event itself as the call.
	Calls string `json:"calls,omitempty"`
	// Name is the path to a call's tool name, and Tool the name that marks
	// the call as a wakeup.
	Name string `json:"name"`
	Tool string `json:"tool"`
	// ID is the path to the call's id, which Rejected matches its result
	// against.
	ID string `json:"id,omitempty"`
	// Input is the path to the call's input object. Empty reads the call
	// itself as its input.
	Input string `json:"input,omitempty"`
	// Delay is the path in the input to the delay in seconds. It is clamped
	// to between one minute and one hour.
	Delay string `json:"delay"`
	// Prompt and Reason are the paths in the input to the prompt the session
	// is resumed with and the reason the job shows while it waits.
	Prompt string `json:"prompt,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Stop is the path in the input to a boolean that, when true, cancels
	// the wakeups scheduled before it rather than scheduling one.
	Stop string `json:"stop,omitempty"`
	// Rejected describes the result that reports the call failed, which
	// cancels it. It needs ID.
	Rejected *WakeupRejection `json:"rejected,omitempty"`
}

// WakeupRejection says where the agent reports that a wakeup call failed,
// such as Claude's tool_result with is_error set.
type WakeupRejection struct {
	// Event is the type of the events that carry results, read through the
	// Wakeup's own Type path. Empty reads results out of every event.
	Event string `json:"event,omitempty"`
	// Results is the path to each result an event carries. Empty reads the
	// event itself as the result.
	Results string `json:"results,omitempty"`
	// ID is the path to the id of the call a result answers, and Error the
	// path to the boolean that marks it failed.
	ID    string `json:"id"`
	Error string `json:"error"`
}

func (w Wakeup) validate() error {
	paths := []struct {
		field, path string
		required    bool
	}{
		{"wakeup.type", w.Type, false},
		{"wakeup.calls", w.Calls, false},
		{"wakeup.name", w.Name, true},
		{"wakeup.id", w.ID, false},
		{"wakeup.input", w.Input, false},
		{"wakeup.delay", w.Delay, true},
		{"wakeup.prompt", w.Prompt, false},
		{"wakeup.reason", w.Reason, false},
		{"wakeup.stop", w.Stop, false},
	}
	if w.Rejected != nil {
		paths = append(paths, []struct {
			field, path string
			required    bool
		}{
			{"wakeup.rejected.results", w.Rejected.Results, false},
			{"wakeup.rejected.id", w.Rejected.ID, true},
			{"wakeup.rejected.error", w.Rejected.Error, true},
		}...)
	}
	for _, p := range paths {
		if p.path == "" {
			if p.required {
				return fmt.Errorf("field %q is required", p.field)
			}
			continue
		}
		if !jsonlPathPattern.MatchString(p.path) {
			return fmt.Errorf("field %q: %q is not a field path", p.field, p.path)
		}
	}
	if w.Tool == "" {
		return fmt.Errorf(`field "wakeup.tool" is required`)
	}
	if w.Type == "" && (w.Event != "" || (w.Rejected != nil && w.Rejected.Event != "")) {
		return fmt.Errorf(`field "wakeup.type" is required to match an event type`)
	}
	if w.Rejected != nil && w.ID == "" {
		return fmt.Errorf(`field "wakeup.rejected" needs "wakeup.id"`)
	}
	return nil
}
