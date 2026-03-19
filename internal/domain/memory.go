package domain

import "time"

// MemoryPool represents a named memory store shared by one or more agents.
type MemoryPool struct {
	ID   string `json:"id"`
	Name string `json:"name"` // "shared", "private:<agent-id>", or custom
}

// MemoryEntry is a single entry in a memory pool (stored as JSONL).
type MemoryEntry struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id,omitempty"`
	Role      string    `json:"role"`    // "user", "assistant", "summary"
	Content   string    `json:"content"` // text content
	Tokens    int       `json:"tokens"`  // estimated token count
	Timestamp time.Time `json:"timestamp"`
}
