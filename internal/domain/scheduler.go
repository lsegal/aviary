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
	Prompt      string      `json:"prompt"`
	Script      string      `json:"script,omitempty"`
	Target      string      `json:"target,omitempty"`
	// Project and RunAgent are a project task's watched project and the agent
	// that runs it, optionally with a model ("claude:opus"); an empty RunAgent
	// uses the project's agent.
	Project  string `json:"project,omitempty"`
	RunAgent string `json:"run_agent,omitempty"`
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
	// Reason is what fired the task: schedule, missed, watch or manual.
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
	ID             string     `json:"id"`
	TaskID         string     `json:"task_id"`
	TaskType       string     `json:"task_type,omitempty"`
	AgentID        string     `json:"agent_id"`
	SessionID      string     `json:"session_id,omitempty"`
	Prompt         string     `json:"prompt"`
	Script         string     `json:"script,omitempty"`
	OutputChannel  string     `json:"output_channel,omitempty"`
	Status         JobStatus  `json:"status"`
	Attempts       int        `json:"attempts"`
	MaxRetries     int        `json:"max_retries"`
	LockedAt       *time.Time `json:"locked_at,omitempty"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	ScheduledFor   *time.Time `json:"scheduled_for,omitempty"`
	ReplyAgentID   string     `json:"reply_agent_id,omitempty"`   // agent whose session to reply to
	ReplySessionID string     `json:"reply_session_id,omitempty"` // session to send output back to
	Output         string     `json:"output,omitempty"`
	// ResumeNote is set when a safe restart interrupted the job. The resumed
	// run receives it ahead of the original prompt in the same session.
	ResumeNote string    `json:"resume_note,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
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
