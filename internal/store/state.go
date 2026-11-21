package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	// InstanceID names this Aviary instance. It is generated on first run and
	// kept so the instance signs its Code Loop claims with the same identity
	// across restarts.
	InstanceID string `json:"instance_id,omitempty"`
}

// instanceIDMu serializes the read-generate-write of the instance ID so two
// callers in one process cannot mint different IDs.
var instanceIDMu sync.Mutex

// InstanceID returns this Aviary instance's persisted ID, generating and
// saving an 8-character uppercase hex ID on first run.
func InstanceID() (string, error) {
	instanceIDMu.Lock()
	defer instanceIDMu.Unlock()
	state, err := ReadAppState()
	if err != nil {
		return "", err
	}
	if state.InstanceID != "" {
		return state.InstanceID, nil
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("create instance id: %w", err)
	}
	state.InstanceID = strings.ToUpper(hex.EncodeToString(b))
	if err := WriteAppState(state); err != nil {
		return "", err
	}
	return state.InstanceID, nil
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

	channelKey := func(agentName string, ch config.ChannelConfig) string {
		return fmt.Sprintf("%s/%s/%s", agentName, ch.Type, strings.TrimSpace(ch.ID))
	}

	prevChannels := map[string]config.ChannelConfig{}
	if prevCfg != nil {
		for _, agentCfg := range prevCfg.Agents {
			for _, ch := range agentCfg.Channels {
				prevChannels[channelKey(agentCfg.Name, ch)] = ch
			}
		}
	}
	nextChannels := make(map[string]struct{})

	for _, agentCfg := range nextCfg.Agents {
		for _, ch := range agentCfg.Channels {
			key := channelKey(agentCfg.Name, ch)
			nextChannels[key] = struct{}{}
			meta := state.Channels[key]
			enabled := config.BoolOr(ch.Enabled, true)
			prevCh, existedBefore := prevChannels[key]
			if existedBefore {
				prevEnabled := config.BoolOr(prevCh.Enabled, true)
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
				meta.EnabledAt = now
			} else {
				meta.DisabledAt = now
			}
			state.Channels[key] = meta
		}
	}

	for key, prevCh := range prevChannels {
		if _, stillConfigured := nextChannels[key]; stillConfigured {
			continue
		}
		meta := state.Channels[key]
		if config.BoolOr(prevCh.Enabled, true) && !meta.DisabledAt.After(meta.EnabledAt) {
			meta.DisabledAt = now
			state.Channels[key] = meta
		}
	}

	return WriteAppState(state)
}
