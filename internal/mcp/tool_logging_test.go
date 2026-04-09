package mcp

import "testing"

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
