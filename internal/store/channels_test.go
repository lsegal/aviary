package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func setupStoreDir(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	SetDataDir(tmp)
	t.Cleanup(func() { SetDataDir("") })
}

// TestReadSessionChannels_Missing verifies that a missing file returns an
// empty config (not an error).
func TestReadSessionChannels_Missing(t *testing.T) {
	setupStoreDir(t)

	cfg, err := ReadSessionChannels("agent_foo", "sess1")
	assert.NoError(t, err)
	assert.Equal(t, "sess1", cfg.SessionID)
	assert.Equal(t, "agent_foo", cfg.AgentID)
	assert.Equal(t, 0, len(cfg.Channels))

}

// TestReadWriteSessionChannels verifies a round-trip persists correctly.
func TestReadWriteSessionChannels(t *testing.T) {
	setupStoreDir(t)

	orig := &SessionChannelsConfig{
		SessionID: "sess42",
		AgentID:   "agent_bot",
		Channels: []SessionChannel{
			{Type: "signal", ID: "+15551234567"},
			{Type: "slack", ID: "C123456"},
		},
	}
	err := WriteSessionChannels(orig)
	assert.NoError(t, err)

	got, err := ReadSessionChannels("agent_bot", "sess42")
	assert.NoError(t, err)
	assert.Equal(t, orig.SessionID, got.SessionID)
	assert.Equal(t, orig.AgentID, got.AgentID)
	assert.Equal(t, 2, len(got.Channels))
	assert.Equal(t, "signal", got.Channels[0].Type)
	assert.Equal(t, "+15551234567", got.Channels[0].ID)

}

// TestWriteSessionChannels_CreatesDirs verifies parent directories are
// created automatically.
func TestWriteSessionChannels_CreatesDirs(t *testing.T) {
	setupStoreDir(t)

	cfg := &SessionChannelsConfig{
		SessionID: "newsess",
		AgentID:   "agent_newbot",
		Channels:  []SessionChannel{{Type: "discord", ID: "987654321"}},
	}
	err := WriteSessionChannels(cfg)
	assert.NoError(t, err)

	p := SessionChannelsPath("agent_newbot", "newsess")
	_, err = os.Stat(p)
	assert.NoError(t, err)

}

// TestEnsureSessionChannel_IdempotentAdd verifies EnsureSessionChannel adds
// only once.
func TestEnsureSessionChannel_IdempotentAdd(t *testing.T) {
	setupStoreDir(t)

	const (
		agentID   = "agent_testbot"
		sessionID = "sess-idem"
		chanType  = "signal"
		chanID    = "+15550000001"
	)

	// First call should add.
	err := EnsureSessionChannel(agentID, sessionID, chanType, chanID)
	assert.NoError(t, err)

	cfg, _ := ReadSessionChannels(agentID, sessionID)
	assert.Equal(t, 1, len(cfg.Channels))

	// Second call should be a no-op.
	err = EnsureSessionChannel(agentID, sessionID, chanType, chanID)
	assert.NoError(t, err)

	cfg, _ = ReadSessionChannels(agentID, sessionID)
	assert.Equal(t, 1, len(cfg.Channels))

}

// TestEnsureSessionChannel_PreservesExisting verifies adding a new channel
// does not overwrite an existing one.
func TestEnsureSessionChannel_PreservesExisting(t *testing.T) {
	setupStoreDir(t)

	const agentID, sessionID = "agent_multi", "sess-multi"
	err := EnsureSessionChannel(agentID, sessionID, "signal", "+1111")
	assert.NoError(t, err)

	err = EnsureSessionChannel(agentID, sessionID, "slack", "CSLACK1")
	assert.NoError(t, err)

	cfg, err := ReadSessionChannels(agentID, sessionID)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(cfg.Channels))

}

// TestFindAllSessionChannelsConfigs scans multiple agent dirs.
func TestFindAllSessionChannelsConfigs(t *testing.T) {
	setupStoreDir(t)

	// Create channel configs for two different agents.
	cfgs := []*SessionChannelsConfig{
		{SessionID: "s1", AgentID: "agent_alpha", Channels: []SessionChannel{{Type: "signal", ID: "+1"}}},
		{SessionID: "s2", AgentID: "agent_beta", Channels: []SessionChannel{{Type: "slack", ID: "C1"}}},
	}
	for _, c := range cfgs {
		err := WriteSessionChannels(c)
		assert.NoError(t, err)

	}

	found, err := FindAllSessionChannelsConfigs()
	assert.NoError(t, err)
	assert.Equal(t, 2, len(found))

}

// TestFindAllSessionChannelsConfigs_SkipsInvalid verifies that malformed JSON
// files are silently skipped.
func TestFindAllSessionChannelsConfigs_SkipsInvalid(t *testing.T) {
	setupStoreDir(t)

	// Write a valid config.
	valid := &SessionChannelsConfig{
		SessionID: "good",
		AgentID:   "agent_valid",
		Channels:  []SessionChannel{{Type: "signal", ID: "+2"}},
	}
	err := WriteSessionChannels(valid)
	assert.NoError(t, err)

	// Write a malformed .channels.json file.
	sessDir := filepath.Join(AgentDir("agent_valid"), "sessions")
	badFile := filepath.Join(sessDir, "bad.channels.json")
	err = os.WriteFile(badFile, []byte("{not valid json}"), 0o600)
	assert.NoError(t, err)

	found, err := FindAllSessionChannelsConfigs()
	assert.NoError(t, err)
	assert.Equal(t, // Only the valid one should be returned.
		1, len(found))

}

// TestFindAllSessionChannelsConfigs_Empty verifies empty data dir returns nil.
func TestFindAllSessionChannelsConfigs_Empty(t *testing.T) {
	setupStoreDir(t)

	found, err := FindAllSessionChannelsConfigs()
	assert.NoError(t, err)
	assert.Equal(t, 0, len(found))

}

// TestSessionChannelsPath verifies the path follows the same naming as SessionPath.
func TestSessionChannelsPath(t *testing.T) {
	setupStoreDir(t)

	p := SessionChannelsPath("agent_foo", "sess1")
	sessP := SessionPath("agent_foo", "sess1")
	assert.

		// channels path should share the same base as the session path.
		Equal(t, filepath.Dir(sessP), filepath.Dir(p))
	assert.Equal(t, // channels path should end with .channels.json.
		".json", filepath.Ext(p))

}
