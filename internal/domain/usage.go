package domain

import "time"

// UsageRecord captures token usage for a single LLM streaming call.
// One record is written per agent.Prompt() invocation (after all tool rounds).
// A record whose Project is set instead records Projects work: a job run's
// wall time or an issue's outcome, and carries no token use.
type UsageRecord struct {
	Timestamp        time.Time     `json:"timestamp"`
	SessionID        string        `json:"session_id"`
	AgentID          string        `json:"agent_id"`
	Model            string        `json:"model"`
	Provider         string        `json:"provider"`
	InputTokens      int           `json:"input_tokens"`
	OutputTokens     int           `json:"output_tokens"`
	CacheReadTokens  int           `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int           `json:"cache_write_tokens,omitempty"`
	ToolCalls        int           `json:"tool_calls,omitempty"`
	CostUSD          float64       `json:"cost_usd,omitempty"` // reported by CLI backends
	HasError         bool          `json:"has_error,omitempty"`
	HasThrottle      bool          `json:"has_throttle,omitempty"`
	Project          *ProjectUsage `json:"project,omitempty"`
	// Source names what spent the tokens when it is not a session's prompt,
	// such as UsageSourceTaskEditor.
	Source string `json:"source,omitempty"`
}

// UsageSourceTaskEditor marks the tokens a task editor's live determinism
// analysis spent, attributed to the editor rather than to any task.
const UsageSourceTaskEditor = "task_editor"

// The outcomes a ProjectUsage records for an issue.
const (
	ProjectOutcomeMerged = "merged"
	ProjectOutcomeFailed = "failed"
)

// ProjectUsage is the Projects work one usage record reports: a job run on
// Issue that took DurationMS of wall time, or Issue's Outcome. A merged
// outcome also carries the Additions and Deletions of the pull request that
// merged.
type ProjectUsage struct {
	// Issue is the lowercase "owner/repo#number" of the issue worked on.
	Issue      string `json:"issue"`
	Outcome    string `json:"outcome,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Additions  int    `json:"additions,omitempty"`
	Deletions  int    `json:"deletions,omitempty"`
}
