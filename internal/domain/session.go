package domain

import "time"

// Session represents a conversation with an agent.
type Session struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Name      string    `json:"name,omitempty"` // human-readable name; "main" for the default session
	TaskID    string    `json:"task_id,omitempty"` // set for task sessions
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MessageRole identifies who sent a message.
type MessageRole string

const (
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleSystem    MessageRole = "system"
)

// Message represents a single message in a session.
type Message struct {
	ID        string      `json:"id"`
	SessionID string      `json:"session_id"`
	Role      MessageRole `json:"role"`
	Content   string      `json:"content"`
	MediaURL  string      `json:"media_url,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}
