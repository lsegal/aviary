package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SessionChannel represents a delivery target for a session's responses.
type SessionChannel struct {
	Type string `json:"type"` // e.g. "signal", "slack"
	ID   string `json:"id"`   // channel/conversation ID (phone number, Slack channel ID, etc.)
}

// SessionChannelsConfig is the content of a session's .channels.json sidecar file.
// It records which channels should receive outgoing responses for that session.
type SessionChannelsConfig struct {
	SessionID string           `json:"session_id"`
	AgentID   string           `json:"agent_id"`
	Channels  []SessionChannel `json:"channels"`
}

// ReadSessionChannels reads the channel delivery config for a session.
// Returns an empty config (not an error) when the file does not exist yet.
func ReadSessionChannels(agentID, sessionID string) (*SessionChannelsConfig, error) {
	path := SessionChannelsPath(agentID, sessionID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &SessionChannelsConfig{SessionID: sessionID, AgentID: agentID}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg SessionChannelsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// WriteSessionChannels persists the channel delivery config for a session.
func WriteSessionChannels(cfg *SessionChannelsConfig) error {
	path := SessionChannelsPath(cfg.AgentID, cfg.SessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// EnsureSessionChannel adds channelType/channelID to the session's channel config
// if it is not already present, then writes the file. It is a no-op when the
// channel is already listed.
func EnsureSessionChannel(agentID, sessionID, channelType, channelID string) error {
	cfg, err := ReadSessionChannels(agentID, sessionID)
	if err != nil {
		return err
	}
	for _, ch := range cfg.Channels {
		if ch.Type == channelType && ch.ID == channelID {
			return nil // already present
		}
	}
	cfg.Channels = append(cfg.Channels, SessionChannel{Type: channelType, ID: channelID})
	return WriteSessionChannels(cfg)
}

// FindAllSessionChannelsConfigs scans every agent's sessions directory and
// returns all existing session channel configs.
func FindAllSessionChannelsConfigs() ([]*SessionChannelsConfig, error) {
	agentsDir := filepath.Join(DataDir(), DirAgents)
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result []*SessionChannelsConfig
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sessionsDir := filepath.Join(agentsDir, e.Name(), "sessions")
		sessEntries, err := os.ReadDir(sessionsDir)
		if err != nil {
			continue
		}
		for _, se := range sessEntries {
			if se.IsDir() || !strings.HasSuffix(se.Name(), ".channels.json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(sessionsDir, se.Name()))
			if err != nil {
				continue
			}
			var cfg SessionChannelsConfig
			if err := json.Unmarshal(data, &cfg); err != nil {
				continue
			}
			if cfg.SessionID != "" && cfg.AgentID != "" {
				result = append(result, &cfg)
			}
		}
	}
	return result, nil
}
