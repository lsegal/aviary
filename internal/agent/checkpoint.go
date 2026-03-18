package agent

import "time"

// RunCheckpoint stores the state of an in-flight agent prompt so it can be
// resumed after a server restart or config reload. Checkpoint files are written
// to <agentDir>/checkpoints/ at the start of each prompt and deleted on completion.
// If a checkpoint is older than the configured failed_task_timeout when the
// server restarts, the agent notifies the session that it gave up instead of
// resuming.
type RunCheckpoint struct {
	// AgentName is the name of the agent that owns this checkpoint.
	AgentName string `json:"agent_name"`
	// SessionID is the session the prompt was running in.
	SessionID string `json:"session_id"`
	// Message is the original user message to re-issue.
	Message string `json:"message"`
	// MediaURL is an optional media attachment for the message.
	MediaURL string `json:"media_url,omitempty"`
	// Overrides holds any per-run model/tool overrides that were active.
	Overrides RunOverrides `json:"overrides,omitempty"`
	// CreatedAt is when the checkpoint was written (prompt start time).
	CreatedAt time.Time `json:"created_at"`
	// RetryCount tracks how many times this checkpoint has been re-issued.
	RetryCount int `json:"retry_count,omitempty"`
}
