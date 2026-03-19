package domain

import (
	"strings"
	"time"
)

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

// MessageSender identifies the user who authored a session message.
// Participant=false marks context-only users whose messages should be
// retained in history but not treated as active conversation participants.
type MessageSender struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	Participant bool   `json:"participant"`
}

// NewMessageSender constructs a normalized sender payload.
func NewMessageSender(id, name string, participant bool) *MessageSender {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)
	if id == "" && name == "" {
		return nil
	}
	if name == "" {
		name = id
	}
	return &MessageSender{ID: id, Name: name, Participant: participant}
}

// Message represents a single message in a session.
type Message struct {
	ID         string         `json:"id"`
	Role       MessageRole    `json:"role"`
	Sender     *MessageSender `json:"sender,omitempty"`
	Content    string         `json:"content"`
	MediaURL   string         `json:"media_url,omitempty"`
	Model      string         `json:"model,omitempty"`       // LLM model used; only set on assistant messages
	ResponseID string         `json:"response_id,omitempty"` // ID of the assistant message that responded to this prompt
	Timestamp  time.Time      `json:"timestamp"`
}
