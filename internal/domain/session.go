package domain

import "time"

// SessionType identifies the kind of session.
type SessionType string

// SessionType values.
const (
	SessionTypeMain    SessionType = "main"
	SessionTypeChannel SessionType = "channel"
	SessionTypeTask    SessionType = "task"
	SessionTypeUser    SessionType = "user"
)

// Session represents a conversation with an agent.
type Session struct {
	ID        string      `json:"id,omitempty"`
	AgentID   string      `json:"agent_id,omitempty"`
	Name      string      `json:"name,omitempty"`    // human-readable name; "main" for the default session
	TaskID    string      `json:"task_id,omitempty"` // set for task sessions
	Type      SessionType `json:"type,omitempty"`    // main, channel, task, or user (default)
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// MessageRole identifies who sent a message.
type MessageRole string

// MessageRole values.
const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
	MessageRoleTool      MessageRole = "tool"
)

// Message represents a single message in a session.
type Message struct {
	ID        string      `json:"id"`
	SessionID string      `json:"session_id,omitempty"`
	Role      MessageRole `json:"role"`
	Content   string      `json:"content"`
	MediaURL  string      `json:"media_url,omitempty"`
	Model     string      `json:"model,omitempty"` // LLM model used; only set on assistant messages
	Timestamp time.Time   `json:"timestamp"`
}
