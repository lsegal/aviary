package domain

import "time"

// UsageRecord captures token usage for a single LLM streaming call.
// One record is written per agent.Prompt() invocation (after all tool rounds).
type UsageRecord struct {
	Timestamp        time.Time `json:"timestamp"`
	SessionID        string    `json:"session_id"`
	AgentName        string    `json:"agent_name"`
	Model            string    `json:"model"`
	Provider         string    `json:"provider"`
	InputTokens      int       `json:"input_tokens"`
	OutputTokens     int       `json:"output_tokens"`
	CacheReadTokens  int       `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int       `json:"cache_write_tokens,omitempty"`
	ToolCalls        int       `json:"tool_calls,omitempty"`
	HasError         bool      `json:"has_error,omitempty"`
}
