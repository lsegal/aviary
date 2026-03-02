// Package domain defines the core domain entities for Aviary.
package domain

import "time"

// AgentState represents the runtime state of an agent.
type AgentState string

const (
	AgentStateIdle    AgentState = "idle"
	AgentStateRunning AgentState = "running"
	AgentStateStopped AgentState = "stopped"
)

// Agent represents an autonomous agent instance.
type Agent struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Model     string     `json:"model"`
	Fallbacks []string   `json:"fallbacks,omitempty"`
	State     AgentState `json:"state"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
