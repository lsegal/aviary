package domain

import "time"

// TriggerType identifies how a task is triggered.
type TriggerType string

// TriggerType values.
const (
	TriggerTypeOnce     TriggerType = "once"
	TriggerTypeInterval TriggerType = "interval"
	TriggerTypeDaily    TriggerType = "daily"
	TriggerTypeWeekly   TriggerType = "weekly"
	TriggerTypeCron     TriggerType = "cron"
	TriggerTypeWatch    TriggerType = "watch"
	// TriggerTypeMessage marks a workflow: a task that runs when a matching
	// message arrives on one of its agent's channels.
	TriggerTypeMessage TriggerType = "message"
)

// ScheduledTask represents a task to be executed by an agent on a schedule or trigger.
type ScheduledTask struct {
	ID          string      `json:"id"`
	AgentName   string      `json:"agent_name"`
	AgentID     string      `json:"agent_id"`
	Name        string      `json:"name"`
	Type        string      `json:"type,omitempty"`
	TriggerType TriggerType `json:"trigger_type"`
	Schedule    string      `json:"schedule,omitempty"` // cron expression
	StartAt     *time.Time  `json:"start_at,omitempty"`
	RunOnce     bool        `json:"run_once,omitempty"`
	Every       string      `json:"every,omitempty"`
	At          string      `json:"at,omitempty"`
	Days        string      `json:"days,omitempty"`
	Timezone    string      `json:"timezone,omitempty"`
	Overlap     string      `json:"overlap,omitempty"`
	Missed      string      `json:"missed,omitempty"`
	Watch       string      `json:"watch,omitempty"` // glob pattern
	// Channel, Entry and Match are a workflow's trigger: the channel and
	// conversation it watches and the wildcard its messages must match.
	Channel string `json:"channel,omitempty"`
	Entry   string `json:"entry,omitempty"`
	Match   string `json:"match,omitempty"`
	Prompt  string `json:"prompt"`
	Script  string `json:"script,omitempty"`
	Target  string `json:"target,omitempty"`
	// Project is a project task's watched project, and Model overrides the
	// project agent's model for its runs; an empty Model uses the agent's own.
	Project string `json:"project,omitempty"`
	Model   string `json:"model,omitempty"`
	// Precompile is set on a project task whose runs try its compiled script
	// first, and Compiled when it has one, its prompt having passed the
	// determinism gate.
	Precompile bool `json:"precompile,omitempty"`
	Compiled   bool `json:"compiled,omitempty"`
	// Enabled is false for a task turned off in config, which never fires.
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	// Description summarizes the trigger, e.g. "daily at 09:00 (UTC)".
	Description string     `json:"description,omitempty"`
	NextRun     *time.Time `json:"next_run,omitempty"`
	LastFire    *time.Time `json:"last_fire,omitempty"`
	// History holds the most recent fires, oldest first.
	History []TaskRun `json:"history,omitempty"`
}

// TaskRunStatus is the outcome of one scheduled-task fire.
type TaskRunStatus string

// TaskRunStatus values.
const (
	TaskRunQueued    TaskRunStatus = "queued"
	TaskRunRunning   TaskRunStatus = "running"
	TaskRunRetrying  TaskRunStatus = "retrying"
	TaskRunCompleted TaskRunStatus = "completed"
	TaskRunFailed    TaskRunStatus = "failed"
	TaskRunCanceled  TaskRunStatus = "canceled"
	// TaskRunSkipped marks a fire dropped by the overlap policy.
	TaskRunSkipped TaskRunStatus = "skipped"
	// TaskRunMissed marks fires that were due while Aviary was stopped or the
	// machine slept, and were dropped by the missed-run policy.
	TaskRunMissed TaskRunStatus = "missed"
)

// TaskRun is one entry of a scheduled task's run history.
type TaskRun struct {
	FireTime time.Time `json:"fire_time"`
	// Reason is what fired the task: schedule, missed, watch, message or
	// manual.
	Reason     string        `json:"reason"`
	Status     TaskRunStatus `json:"status"`
	JobID      string        `json:"job_id,omitempty"`
	Summary    string        `json:"summary,omitempty"`
	DurationMS int64         `json:"duration_ms,omitempty"`
}

// JobStatus represents the lifecycle state of a job.
type JobStatus string

// JobStatus values.
const (
	JobStatusPending    JobStatus = "pending"
	JobStatusInProgress JobStatus = "in_progress"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusCanceled   JobStatus = "canceled"
)

// Job represents a single instance of a task being executed.
type Job struct {
	ID            string `json:"id"`
	TaskID        string `json:"task_id"`
	TaskType      string `json:"task_type,omitempty"`
	AgentID       string `json:"agent_id"`
	SessionID     string `json:"session_id,omitempty"`
	Prompt        string `json:"prompt"`
	Script        string `json:"script,omitempty"`
	OutputChannel string `json:"output_channel,omitempty"`
	// Trigger is what fired the run: schedule, missed (a catch-up), watch,
	// message or manual. Empty for jobs no task fire created.
	Trigger string `json:"trigger,omitempty"`
	// TriggerContext is the channel message that started a workflow run.
	TriggerContext *TriggerContext `json:"trigger_context,omitempty"`
	Status         JobStatus       `json:"status"`
	Attempts       int             `json:"attempts"`
	MaxRetries     int             `json:"max_retries"`
	LockedAt       *time.Time      `json:"locked_at,omitempty"`
	NextRetryAt    *time.Time      `json:"next_retry_at,omitempty"`
	ScheduledFor   *time.Time      `json:"scheduled_for,omitempty"`
	ReplyAgentID   string          `json:"reply_agent_id,omitempty"`   // agent whose session to reply to
	ReplySessionID string          `json:"reply_session_id,omitempty"` // session to send output back to
	Output         string          `json:"output,omitempty"`
	// Error is why the latest attempt failed, shown with the run; it is
	// cleared when an attempt completes.
	Error string `json:"error,omitempty"`
	// ScriptError is set when a precompiled project task's script failed and
	// the model ran the task instead: the script's full error, which the
	// run's logs show and its summary leaves out.
	ScriptError string `json:"script_error,omitempty"`
	// ResumeNote is set when a safe restart interrupted the job. The resumed
	// run receives it ahead of the original prompt in the same session.
	ResumeNote string    `json:"resume_note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// TriggerContext records the channel message that started a workflow run,
// for debugging the run. Senders are recorded by display name only, never by
// a raw platform ID or phone number.
type TriggerContext struct {
	// ChannelType, ConfiguredID and Channel name the agent's channel the
	// message arrived on; Entry is the conversation on it and ThreadTS the
	// platform thread, when there is one.
	ChannelType  string `json:"channel_type"`
	ConfiguredID string `json:"configured_id"`
	Channel      string `json:"channel"`
	Entry        string `json:"entry"`
	ThreadTS     string `json:"thread_ts,omitempty"`
	// SessionID is the channel session the message was recorded in.
	SessionID string `json:"session_id,omitempty"`
	// Message is the triggering message; Before and After are the messages
	// just before and just after it in the conversation.
	Message TriggerMessage   `json:"message"`
	Before  []TriggerMessage `json:"before,omitempty"`
	After   []TriggerMessage `json:"after,omitempty"`
}

// TriggerMessage is one channel message in a TriggerContext.
type TriggerMessage struct {
	// Sender is the sender's display name, empty when only a raw ID is known.
	Sender      string    `json:"sender,omitempty"`
	FromAgent   bool      `json:"from_agent,omitempty"`
	Text        string    `json:"text"`
	Attachments []string  `json:"attachments,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// RunStatus represents the status of an individual job execution.
type RunStatus string

// RunStatus values.
const (
	RunStatusPending    RunStatus = "pending"
	RunStatusInProgress RunStatus = "in_progress"
	RunStatusCompleted  RunStatus = "completed"
	RunStatusFailed     RunStatus = "failed"
)

// Run represents a single execution attempt of a Job.
type Run struct {
	ID        string     `json:"id"`
	JobID     string     `json:"job_id"`
	Status    RunStatus  `json:"status"`
	Output    string     `json:"output,omitempty"`
	Error     string     `json:"error,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

// TaskCompileStatus describes the outcome of a task precompile attempt.
type TaskCompileStatus string

// TaskCompileStatus values.
const (
	TaskCompileStatusSucceeded TaskCompileStatus = "succeeded"
	TaskCompileStatusSkipped   TaskCompileStatus = "skipped"
	TaskCompileStatusFailed    TaskCompileStatus = "failed"
)

// TaskCompileStage captures a single compiler stage interaction.
type TaskCompileStage struct {
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	SystemPrompt string     `json:"system_prompt,omitempty"`
	UserPrompt   string     `json:"user_prompt,omitempty"`
	Response     string     `json:"response,omitempty"`
	Error        string     `json:"error,omitempty"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
}

// TaskCompileStep summarizes a compiler-analyzed task step.
type TaskCompileStep struct {
	Kind          string `json:"kind"`
	Deterministic bool   `json:"deterministic"`
	Tool          string `json:"tool,omitempty"`
	Description   string `json:"description"`
}

// TaskCompile records a single attempt to precompile a scheduled prompt task.
type TaskCompile struct {
	ID                 string             `json:"id"`
	AgentID            string             `json:"agent_id"`
	TaskName           string             `json:"task_name,omitempty"`
	RequestedTaskType  string             `json:"requested_task_type,omitempty"`
	ResultTaskType     string             `json:"result_task_type,omitempty"`
	Trigger            string             `json:"trigger,omitempty"`
	Target             string             `json:"target,omitempty"`
	Prompt             string             `json:"prompt,omitempty"`
	RunDiscovery       bool               `json:"run_discovery,omitempty"`
	NeedsDiscovery     bool               `json:"needs_discovery,omitempty"`
	DeterministicSteps int                `json:"deterministic_steps,omitempty"`
	Validated          bool               `json:"validated,omitempty"`
	Status             TaskCompileStatus  `json:"status"`
	Reason             string             `json:"reason,omitempty"`
	Steps              []TaskCompileStep  `json:"steps,omitempty"`
	Script             string             `json:"script,omitempty"`
	Stages             []TaskCompileStage `json:"stages,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
}
