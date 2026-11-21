package mcp

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestRedactedJSONRedactsAuthSetValue(t *testing.T) {
	got := redactedJSON(map[string]any{
		"name":  "openai:default",
		"value": "sk-test-secret",
	})
	if got != `{"name":"openai:default","value":"[REDACTED]"}` && got != `{"value":"[REDACTED]","name":"openai:default"}` {
		t.Fatalf("expected auth value to be redacted, got %s", got)
	}
}

func TestRedactedJSONRedactsOAuthCode(t *testing.T) {
	got := redactedJSON(map[string]any{
		"code": "oauth-code",
	})
	if got != `{"code":"[REDACTED]"}` {
		t.Fatalf("expected oauth code to be redacted, got %s", got)
	}
}

func TestLogToolCallSkipsProjectsTools(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	logToolCall("inprocess", "project_check", map[string]any{"target": "lsegal/aviary"})
	logToolCall("http", "project_status", nil)
	if got := buf.String(); got != "" {
		t.Fatalf("expected no log line for projects tools, got %q", got)
	}

	logToolCall("inprocess", "task_list", nil)
	if !strings.Contains(buf.String(), "mcp: tool call") {
		t.Fatalf("expected a log line for other tools, got %q", buf.String())
	}
}
