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
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Model       string     `json:"model"`
	Memory      string     `json:"memory"` // pool name: "shared", "private", or custom
	State       AgentState `json:"state"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
