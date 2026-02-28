package config

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed schema.json
var schemaJSON []byte

// Validate checks the config against the embedded JSON schema.
// It performs a lightweight structural check without a full JSON Schema validator.
func Validate(cfg *Config) error {
	// Ensure schema.json is valid JSON (compile-time guard).
	var raw map[string]any
	if err := json.Unmarshal(schemaJSON, &raw); err != nil {
		return fmt.Errorf("internal: bad schema.json: %w", err)
	}

	// Validate required invariants.
	for i, agent := range cfg.Agents {
		if agent.Name == "" {
			return fmt.Errorf("agents[%d]: name is required", i)
		}
		for j, task := range agent.Tasks {
			if task.Name == "" {
				return fmt.Errorf("agents[%d].tasks[%d]: name is required", i, j)
			}
			if task.Prompt == "" {
				return fmt.Errorf("agents[%d].tasks[%d]: prompt is required", i, j)
			}
			if task.Schedule == "" && task.Watch == "" {
				return fmt.Errorf("agents[%d].tasks[%d]: one of schedule or watch is required", i, j)
			}
		}
		for j, ch := range agent.Channels {
			if ch.Type == "" {
				return fmt.Errorf("agents[%d].channels[%d]: type is required", i, j)
			}
		}
	}

	if cfg.Server.Port == 0 {
		cfg.Server.Port = 16677
	}

	return nil
}

// Schema returns the raw JSON schema bytes.
func Schema() []byte {
	return schemaJSON
}
