package agent

import "time"

const (
	maxCheckpointRecoveryRetries = 3
	checkpointRecoveryCooldown   = time.Minute
)

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
	// MediaURLs are the media attachments for the message.
	MediaURLs []string `json:"media_urls,omitempty"`
	// Overrides holds any per-run model/tool overrides that were active.
	Overrides RunOverrides `json:"overrides,omitempty"`
	// CreatedAt is when the checkpoint was written (prompt start time).
	CreatedAt time.Time `json:"created_at"`
	// RetryCount tracks how many times this checkpoint has been re-issued.
	RetryCount int `json:"retry_count,omitempty"`
	// LastRecoveredAt is when the checkpoint was last re-issued.
	LastRecoveredAt time.Time `json:"last_recovered_at,omitempty"`
}

// claimCheckpoint marks path as owned by one of r's in-flight prompts.
func (r *AgentRunner) claimCheckpoint(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.liveCheckpoints == nil {
		r.liveCheckpoints = map[string]struct{}{}
	}
	r.liveCheckpoints[path] = struct{}{}
}

// releaseCheckpoint ends the claim on path once its prompt has finished.
func (r *AgentRunner) releaseCheckpoint(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.liveCheckpoints, path)
}

// ownsCheckpoint reports whether path belongs to one of r's in-flight
// prompts rather than to an interrupted run.
func (r *AgentRunner) ownsCheckpoint(path string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.liveCheckpoints[path]
	return ok
}
