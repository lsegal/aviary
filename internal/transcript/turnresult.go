package transcript

import "encoding/json"

// TurnOutcome is what a stream's final result event says about the turn.
type TurnOutcome int

const (
	// TurnUnfinished means the line is not a final result event.
	TurnUnfinished TurnOutcome = iota
	// TurnSucceeded is a final result event for a turn that completed.
	TurnSucceeded
	// TurnFailed is a final result event for a turn that ended in an error.
	TurnFailed
)

// TurnResult reads line as the final result event a CLI writes in format
// once its turn is over: Claude's and Gemini's "result" and Codex's
// "turn.completed" or "turn.failed". A run whose stream reached a successful
// one finished its work, even when the process it ran in did not exit
// cleanly afterwards (#1122). Formats without such an event report
// TurnUnfinished for every line.
func TurnResult(format string, line []byte) TurnOutcome {
	var envelope struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		IsError bool   `json:"is_error"`
		Status  string `json:"status"`
	}
	if json.Unmarshal(line, &envelope) != nil {
		return TurnUnfinished
	}
	switch format {
	case FormatClaudeStreamJSON, "claude-stream-json":
		if envelope.Type != "result" {
			return TurnUnfinished
		}
		if envelope.IsError || (envelope.Subtype != "" && envelope.Subtype != "success") {
			return TurnFailed
		}
		return TurnSucceeded
	case FormatCodexJSON:
		switch envelope.Type {
		case "turn.completed":
			return TurnSucceeded
		case "turn.failed":
			return TurnFailed
		}
	case FormatGeminiJSON:
		if envelope.Type != "result" {
			return TurnUnfinished
		}
		if envelope.Status == "error" {
			return TurnFailed
		}
		return TurnSucceeded
	}
	return TurnUnfinished
}
