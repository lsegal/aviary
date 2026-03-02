package domain

import "time"

// TriggerType identifies how a task is triggered.
type TriggerType string

const (
	TriggerTypeCron  TriggerType = "cron"
	TriggerTypeWatch TriggerType = "watch"
)

// ScheduledTask represents a task to be executed by an agent on a schedule or trigger.
type ScheduledTask struct {
	ID          string      `json:"id"`
	AgentID     string      `json:"agent_id"`
	Name        string      `json:"name"`
	TriggerType TriggerType `json:"trigger_type"`
	Schedule    string      `json:"schedule,omitempty"` // cron expression
	StartAt     *time.Time  `json:"start_at,omitempty"`
	RunOnce     bool        `json:"run_once,omitempty"`
	Watch       string      `json:"watch,omitempty"`    // glob pattern
	Prompt      string      `json:"prompt"`
	Channel     string      `json:"channel,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

// JobStatus represents the lifecycle state of a job.
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusInProgress JobStatus = "in_progress"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
)

// Job represents a single instance of a task being executed.
type Job struct {
	ID          string     `json:"id"`
	TaskID      string     `json:"task_id"`
	AgentID     string     `json:"agent_id"`
	AgentName   string     `json:"agent_name"`
	Prompt      string     `json:"prompt"`
	Status      JobStatus  `json:"status"`
	Attempts    int        `json:"attempts"`
	MaxRetries  int        `json:"max_retries"`
	LockedAt    *time.Time `json:"locked_at,omitempty"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// RunStatus represents the status of an individual job execution.
type RunStatus string

const (
	RunStatusPending    RunStatus = "pending"
	RunStatusInProgress RunStatus = "in_progress"
	RunStatusCompleted  RunStatus = "completed"
	RunStatusFailed     RunStatus = "failed"
)

// Run represents a single execution attempt of a Job.
type Run struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	Status    RunStatus `json:"status"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}
