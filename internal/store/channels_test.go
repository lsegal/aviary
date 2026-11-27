package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			{Type: "signal", ConfiguredID: "primary", ID: "+15551234567"},
			{Type: "slack", ConfiguredID: "alerts", ID: "C123456"},
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
	assert.Equal(t, "primary", got.Channels[0].ConfiguredID)
	assert.Equal(t, "+15551234567", got.Channels[0].ID)

}

// TestWriteSessionChannels_CreatesDirs verifies parent directories are
// created automatically.
func TestWriteSessionChannels_CreatesDirs(t *testing.T) {
	setupStoreDir(t)

	cfg := &SessionChannelsConfig{
		SessionID: "newsess",
		AgentID:   "agent_newbot",
		Channels:  []SessionChannel{{Type: "discord", ConfiguredID: "ops", ID: "987654321"}},
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
		configID  = "primary"
		chanID    = "+15550000001"
	)

	// First call should add.
	err := EnsureSessionChannel(agentID, sessionID, chanType, configID, chanID)
	assert.NoError(t, err)

	cfg, _ := ReadSessionChannels(agentID, sessionID)
	assert.Equal(t, 1, len(cfg.Channels))

	// Second call should be a no-op.
	err = EnsureSessionChannel(agentID, sessionID, chanType, configID, chanID)
	assert.NoError(t, err)

	cfg, _ = ReadSessionChannels(agentID, sessionID)
	assert.Equal(t, 1, len(cfg.Channels))
	assert.Equal(t, configID, cfg.Channels[0].ConfiguredID)

}

// TestEnsureSessionChannel_PreservesExisting verifies adding a new channel
// does not overwrite an existing one.
func TestEnsureSessionChannel_PreservesExisting(t *testing.T) {
	setupStoreDir(t)

	const agentID, sessionID = "agent_multi", "sess-multi"
	err := EnsureSessionChannel(agentID, sessionID, "signal", "primary", "+1111")
	assert.NoError(t, err)

	err = EnsureSessionChannel(agentID, sessionID, "slack", "alerts", "CSLACK1")
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
		{SessionID: "s1", AgentID: "agent_alpha", Channels: []SessionChannel{{Type: "signal", ConfiguredID: "primary", ID: "+1"}}},
		{SessionID: "s2", AgentID: "agent_beta", Channels: []SessionChannel{{Type: "slack", ConfiguredID: "alerts", ID: "C1"}}},
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
		Channels:  []SessionChannel{{Type: "signal", ConfiguredID: "primary", ID: "+2"}},
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

// TestSetSessionChannel_ReplacesConfig verifies SetSessionChannel writes a
// single target and replaces any previously stored targets.
func TestSetSessionChannel_ReplacesConfig(t *testing.T) {
	setupStoreDir(t)

	const agentID, sessionID = "agent_set", "sess-set"
	require.NoError(t, EnsureSessionChannel(agentID, sessionID, "signal", "primary", "+1111"))
	require.NoError(t, EnsureSessionChannel(agentID, sessionID, "slack", "alerts", "C1"))

	require.NoError(t, SetSessionChannel(agentID, sessionID, "discord", "ops", "D42"))

	got, err := ReadSessionChannels(agentID, sessionID)
	require.NoError(t, err)
	assert.Equal(t, sessionID, got.SessionID)
	assert.Equal(t, agentID, got.AgentID)
	assert.Equal(t, []SessionChannel{{Type: "discord", ConfiguredID: "ops", ID: "D42"}}, got.Channels)
}

// TestSetSessionChannelTarget_PersistsThread verifies the full target,
// including thread metadata, round-trips through the sidecar file.
func TestSetSessionChannelTarget_PersistsThread(t *testing.T) {
	setupStoreDir(t)

	target := SessionChannel{Type: "slack", ConfiguredID: "alerts", ID: "C9", ThreadTS: "1700000000.000100"}
	require.NoError(t, SetSessionChannelTarget("agent_thread", "sess-thread", target))

	got, err := ReadSessionChannels("agent_thread", "sess-thread")
	require.NoError(t, err)
	assert.Equal(t, []SessionChannel{target}, got.Channels)
}

// TestReadSessionChannels_CorruptJSON verifies a malformed sidecar file is
// reported as an error rather than treated as empty.
func TestReadSessionChannels_CorruptJSON(t *testing.T) {
	setupStoreDir(t)

	p := SessionChannelsPath("agent_bad", "sess-bad")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
	require.NoError(t, os.WriteFile(p, []byte("{not json"), 0o600))

	cfg, err := ReadSessionChannels("agent_bad", "sess-bad")
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

// TestReadSessionChannels_ReadError verifies read failures other than a
// missing file are returned. A directory at the sidecar path makes
// os.ReadFile fail on every platform.
func TestReadSessionChannels_ReadError(t *testing.T) {
	setupStoreDir(t)

	require.NoError(t, os.MkdirAll(SessionChannelsPath("agent_dir", "sess-dir"), 0o700))

	cfg, err := ReadSessionChannels("agent_dir", "sess-dir")
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

// TestWriteSessionChannels_MkdirError verifies a failure to create the
// sessions directory is returned. A regular file where the agent directory
// should be blocks the directory creation.
func TestWriteSessionChannels_MkdirError(t *testing.T) {
	setupStoreDir(t)

	agentDir := AgentDir("agent_blocked")
	require.NoError(t, os.MkdirAll(filepath.Dir(agentDir), 0o700))
	require.NoError(t, os.WriteFile(agentDir, []byte("not a dir"), 0o600))

	err := SetSessionChannel("agent_blocked", "sess", "signal", "primary", "+1")
	assert.Error(t, err)
}

// TestWriteSessionChannels_WriteError verifies a failure to write the sidecar
// file is returned and leaves the blocking entry untouched.
func TestWriteSessionChannels_WriteError(t *testing.T) {
	setupStoreDir(t)

	p := SessionChannelsPath("agent_wdir", "sess-wdir")
	require.NoError(t, os.MkdirAll(p, 0o700))

	err := WriteSessionChannels(&SessionChannelsConfig{SessionID: "sess-wdir", AgentID: "agent_wdir"})
	assert.Error(t, err)

	info, statErr := os.Stat(p)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

// TestEnsureSessionChannelTarget_UpdatesThread verifies a matching target with
// different thread metadata is updated in place instead of duplicated.
func TestEnsureSessionChannelTarget_UpdatesThread(t *testing.T) {
	setupStoreDir(t)

	const agentID, sessionID = "agent_ts", "sess-ts"
	other := SessionChannel{Type: "signal", ConfiguredID: "primary", ID: "+1"}
	first := SessionChannel{Type: "slack", ConfiguredID: "alerts", ID: "C1", ThreadTS: "111.1"}
	require.NoError(t, EnsureSessionChannelTarget(agentID, sessionID, other))
	require.NoError(t, EnsureSessionChannelTarget(agentID, sessionID, first))

	updated := first
	updated.ThreadTS = "222.2"
	require.NoError(t, EnsureSessionChannelTarget(agentID, sessionID, updated))

	got, err := ReadSessionChannels(agentID, sessionID)
	require.NoError(t, err)
	assert.Equal(t, []SessionChannel{other, updated}, got.Channels)
}

// TestEnsureSessionChannelTarget_UnchangedSkipsWrite verifies an identical
// target, thread included, leaves the file alone.
func TestEnsureSessionChannelTarget_UnchangedSkipsWrite(t *testing.T) {
	setupStoreDir(t)

	const agentID, sessionID = "agent_same", "sess-same"
	target := SessionChannel{Type: "slack", ConfiguredID: "alerts", ID: "C1", ThreadTS: "111.1"}
	require.NoError(t, EnsureSessionChannelTarget(agentID, sessionID, target))

	// Replace the file with an equivalent but differently formatted payload; a
	// rewrite would reformat it.
	p := SessionChannelsPath(agentID, sessionID)
	compact := []byte(`{"session_id":"sess-same","agent_id":"agent_same","channels":[{"type":"slack","configured_id":"alerts","id":"C1","thread_ts":"111.1"}]}`)
	require.NoError(t, os.WriteFile(p, compact, 0o600))

	require.NoError(t, EnsureSessionChannelTarget(agentID, sessionID, target))

	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, string(compact), string(data))
}

// TestEnsureSessionChannelTarget_ReadError verifies a corrupt existing config
// is reported and not overwritten.
func TestEnsureSessionChannelTarget_ReadError(t *testing.T) {
	setupStoreDir(t)

	p := SessionChannelsPath("agent_corrupt", "sess-corrupt")
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
	require.NoError(t, os.WriteFile(p, []byte("garbage"), 0o600))

	err := EnsureSessionChannel("agent_corrupt", "sess-corrupt", "signal", "primary", "+1")
	assert.Error(t, err)

	data, readErr := os.ReadFile(p)
	require.NoError(t, readErr)
	assert.Equal(t, "garbage", string(data))
}

// TestFindAllSessionChannelsConfigs_AgentsDirError verifies a read failure on
// the agents directory, other than it not existing, is returned. A NUL byte in
// the path is rejected as invalid on every platform; a regular file in place of
// the directory is not portable because Windows reports it as not existing.
func TestFindAllSessionChannelsConfigs_AgentsDirError(t *testing.T) {
	SetDataDir(filepath.Join(t.TempDir(), "bad\x00dir"))
	t.Cleanup(func() { SetDataDir("") })

	found, err := FindAllSessionChannelsConfigs()
	assert.Error(t, err)
	assert.Nil(t, found)
}

// TestFindAllSessionChannelsConfigs_SkipsUnrelatedEntries verifies the scan
// ignores stray files, agents without sessions, non-channel files, nested
// directories, and configs missing their session or agent ID.
func TestFindAllSessionChannelsConfigs_SkipsUnrelatedEntries(t *testing.T) {
	setupStoreDir(t)

	want := &SessionChannelsConfig{
		SessionID: "keep",
		AgentID:   "agent_keep",
		Channels:  []SessionChannel{{Type: "signal", ConfiguredID: "primary", ID: "+3"}},
	}
	require.NoError(t, WriteSessionChannels(want))

	agentsDir := filepath.Join(DataDir(), DirAgents)
	sessDir := filepath.Join(AgentDir("agent_keep"), "sessions")

	// A stray file directly under agents/.
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "stray.txt"), []byte("x"), 0o600))
	// An agent directory with no sessions directory.
	require.NoError(t, os.MkdirAll(AgentDir("agent_nosessions"), 0o700))
	// A non-channel file and a directory with the channel suffix.
	require.NoError(t, os.WriteFile(filepath.Join(sessDir, "keep.jsonl"), []byte("{}\n"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(sessDir, "nested.channels.json"), 0o700))
	// Valid JSON missing the agent or session ID.
	require.NoError(t, os.WriteFile(filepath.Join(sessDir, "noagent.channels.json"), []byte(`{"session_id":"noagent"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(sessDir, "nosession.channels.json"), []byte(`{"agent_id":"agent_keep"}`), 0o600))

	found, err := FindAllSessionChannelsConfigs()
	require.NoError(t, err)
	assert.Equal(t, []*SessionChannelsConfig{want}, found)
}
