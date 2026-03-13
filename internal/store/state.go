package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lsegal/aviary/internal/config"
)

// ChannelMetadata stores lifecycle timestamps for a configured channel.
type ChannelMetadata struct {
	EnabledAt  time.Time `json:"enabled_at,omitempty"`
	DisabledAt time.Time `json:"disabled_at,omitempty"`
}

// AppState stores runtime metadata that should not live in aviary.yaml.
type AppState struct {
	Channels map[string]ChannelMetadata `json:"channels,omitempty"`
}

// StatePath returns the on-disk location for Aviary runtime metadata.
func StatePath() string {
	return filepath.Join(DataDir(), "state.json")
}

// ReadAppState loads runtime metadata from disk, returning an empty state if absent.
func ReadAppState() (*AppState, error) {
	data, err := os.ReadFile(StatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return &AppState{Channels: map[string]ChannelMetadata{}}, nil
		}
		return nil, fmt.Errorf("reading app state: %w", err)
	}
	var state AppState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing app state: %w", err)
	}
	if state.Channels == nil {
		state.Channels = map[string]ChannelMetadata{}
	}
	return &state, nil
}

// WriteAppState persists runtime metadata to disk.
func WriteAppState(state *AppState) error {
	if state == nil {
		state = &AppState{}
	}
	if state.Channels == nil {
		state.Channels = map[string]ChannelMetadata{}
	}
	if err := os.MkdirAll(DataDir(), 0o700); err != nil {
		return fmt.Errorf("creating data dir: %w", err)
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling app state: %w", err)
	}
	if err := os.WriteFile(StatePath(), data, 0o600); err != nil {
		return fmt.Errorf("writing app state: %w", err)
	}
	return nil
}

// UpdateChannelMetadataState updates per-channel lifecycle timestamps in state.json.
func UpdateChannelMetadataState(prevCfg, nextCfg *config.Config, now time.Time) error {
	if nextCfg == nil {
		return nil
	}

	state, err := ReadAppState()
	if err != nil {
		return err
	}
	if state.Channels == nil {
		state.Channels = map[string]ChannelMetadata{}
	}

	prevAgents := make(map[string]config.AgentConfig, len(prevCfg.Agents))
	for _, agentCfg := range prevCfg.Agents {
		prevAgents[agentCfg.Name] = agentCfg
	}

	for _, agentCfg := range nextCfg.Agents {
		prevAgent, ok := prevAgents[agentCfg.Name]
		for ci, ch := range agentCfg.Channels {
			key := fmt.Sprintf("%s/%s/%d", agentCfg.Name, ch.Type, ci)
			meta := state.Channels[key]
			enabled := config.BoolOr(ch.Enabled, true)
			existedBefore := ok && ci < len(prevAgent.Channels)
			if existedBefore {
				prevEnabled := config.BoolOr(prevAgent.Channels[ci].Enabled, true)
				if prevEnabled != enabled {
					if enabled {
						if !meta.EnabledAt.After(meta.DisabledAt) {
							meta.EnabledAt = now
						}
					} else if !meta.DisabledAt.After(meta.EnabledAt) {
						meta.DisabledAt = now
					}
				}
			} else if enabled {
				if meta.EnabledAt.IsZero() {
					meta.EnabledAt = now
				}
			} else if meta.DisabledAt.IsZero() {
				meta.DisabledAt = now
			}
			state.Channels[key] = meta
		}
	}

	return WriteAppState(state)
}
