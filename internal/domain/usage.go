package domain

import "time"

// UsageRecord captures token usage for a single LLM streaming call.
// One record is written per agent.Prompt() invocation (after all tool rounds).
// A record whose Project is set instead records Code Loop work: a job run's
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
	// Task is the "agent/task" key of the scheduled task or workflow whose
	// run spent the tokens, and Channel the channel whose message fired a
	// workflow run.
	Task    string `json:"task,omitempty"`
	Channel string `json:"channel,omitempty"`
	// Delivery is set instead of token counts on a record that logs where a
	// task's or workflow's output was delivered.
	Delivery *JobDelivery `json:"delivery,omitempty"`
	// SessionName, SessionKind and SessionOwner snapshot the session as it
	// was when the tokens were spent, so Usage still names it after the
	// session is deleted. Kind is one of the UsageKind values; Owner is
	// what the session belongs to, such as the issue a Code Loop job worked
	// on, a task's "agent/task" key, or a channel.
	SessionName  string `json:"session_name,omitempty"`
	SessionKind  string `json:"session_kind,omitempty"`
	SessionOwner string `json:"session_owner,omitempty"`
}

// The kinds of session a usage record's SessionKind names.
const (
	UsageKindChat     = "chat"
	UsageKindJob      = "job"
	UsageKindTask     = "task"
	UsageKindWorkflow = "workflow"
)

// DeletedSession is the tombstone of a deleted session, kept so Usage can
// show when the session behind its records was deleted and what it was
// called.
type DeletedSession struct {
	AgentID   string    `json:"agent_id"`
	SessionID string    `json:"session_id"`
	Name      string    `json:"name,omitempty"`
	DeletedAt time.Time `json:"deleted_at"`
}

// UsageSourceTaskEditor marks the tokens a task editor's live determinism
// analysis spent, attributed to the editor rather than to any task.
const UsageSourceTaskEditor = "task_editor"

// The outcomes a ProjectUsage records for an issue.
const (
	ProjectOutcomeMerged = "merged"
	ProjectOutcomeFailed = "failed"
)

// ProjectUsage is the Code Loop work one usage record reports: a job run on
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
