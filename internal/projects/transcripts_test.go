package projects

import (
	"context"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/transcript"
)

// TestBuiltinDefinitionsHaveTranscriptMappers covers #390: a run's recorder is
// built from the definition's transcript format, so the shipped opencode and
// cline definitions record structured transcripts like claude and codex do,
// and so do the gemini (#388) and muse (#397) definitions. The in-process aviary definition
// records core's events directly (#435).
func TestBuiltinDefinitionsHaveTranscriptMappers(t *testing.T) {
	for _, name := range []string{"claude", "codex", "opencode", "cline", "gemini", "muse"} {
		if transcript.NewMapper(builtinDefinition(t, name).Output.TranscriptFormat()) == nil {
			t.Errorf("%s runs record no structured transcript", name)
		}
	}
}

func TestCommandRunnerRecordsJobUsage(t *testing.T) {
	binary, _ := writeFakeAgent(t, `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}
{"type":"result","total_cost_usd":0.25,"usage":{"input_tokens":10,"output_tokens":4,"cache_read_input_tokens":30,"cache_creation_input_tokens":2}}`, 0)
	var records []domain.UsageRecord
	runner := CommandRunner{
		Agent: "claude/opus", ClaudeBinary: binary, Repo: "o/r",
		Transcripts: newTranscriptStore(""),
		RecordUsage: func(rec domain.UsageRecord) { records = append(records, rec) },
	}
	session := AgentSession{ID: "session-7", Agent: "claude/opus", Resume: true}
	if err := runner.RunSession(context.Background(), Issue{Number: 7, Target: "o/r"}, session, func(AgentSession) {}); err != nil {
		t.Fatalf("RunSession() error = %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("usage records = %+v, want one per run", records)
	}
	got := records[0]
	got.Timestamp = time.Time{}
	want := domain.UsageRecord{
		SessionID: "o/r#7", AgentID: UsageAgentID, Model: "opus", Provider: "claude",
		InputTokens: 10, OutputTokens: 4, CacheReadTokens: 30, CacheWriteTokens: 2, ToolCalls: 1, CostUSD: 0.25,
	}
	if got != want {
		t.Fatalf("usage record = %+v, want %+v", got, want)
	}
}
