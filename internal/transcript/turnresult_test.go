package transcript

import "testing"

func TestTurnResult(t *testing.T) {
	cases := []struct {
		format, line string
		want         TurnOutcome
	}{
		{FormatClaudeStreamJSON, `{"type":"result","subtype":"success","is_error":false,"result":"done"}`, TurnSucceeded},
		{FormatClaudeStreamJSON, `{"type":"result","subtype":"error_max_turns","is_error":false}`, TurnFailed},
		{FormatClaudeStreamJSON, `{"type":"result","subtype":"success","is_error":true}`, TurnFailed},
		{FormatClaudeStreamJSON, `{"type":"assistant","message":{"content":[]}}`, TurnUnfinished},
		{FormatCodexJSON, `{"type":"turn.completed","usage":{}}`, TurnSucceeded},
		{FormatCodexJSON, `{"type":"turn.failed","error":{"message":"x"}}`, TurnFailed},
		{FormatGeminiJSON, `{"type":"result","status":"success"}`, TurnSucceeded},
		{FormatGeminiJSON, `{"type":"result","status":"error"}`, TurnFailed},
		{FormatOpencodeJSON, `{"type":"result"}`, TurnUnfinished},
		{FormatClaudeStreamJSON, `not json`, TurnUnfinished},
	}
	for _, c := range cases {
		if got := TurnResult(c.format, []byte(c.line)); got != c.want {
			t.Errorf("TurnResult(%s, %s) = %d, want %d", c.format, c.line, got, c.want)
		}
	}
}
