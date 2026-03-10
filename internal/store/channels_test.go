package store

import (
	"os"
	"path/filepath"
	"testing"
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
	if err != nil {
		t.Fatalf("ReadSessionChannels missing: %v", err)
	}
	if cfg.SessionID != "sess1" {
		t.Fatalf("expected SessionID=sess1, got %q", cfg.SessionID)
	}
	if cfg.AgentID != "agent_foo" {
		t.Fatalf("expected AgentID=agent_foo, got %q", cfg.AgentID)
	}
	if len(cfg.Channels) != 0 {
		t.Fatalf("expected empty channels, got %v", cfg.Channels)
	}
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
	if err := WriteSessionChannels(orig); err != nil {
		t.Fatalf("WriteSessionChannels: %v", err)
	}

	got, err := ReadSessionChannels("agent_bot", "sess42")
	if err != nil {
		t.Fatalf("ReadSessionChannels: %v", err)
	}
	if got.SessionID != orig.SessionID {
		t.Errorf("SessionID mismatch: got %q want %q", got.SessionID, orig.SessionID)
	}
	if got.AgentID != orig.AgentID {
		t.Errorf("AgentID mismatch: got %q want %q", got.AgentID, orig.AgentID)
	}
	if len(got.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(got.Channels))
	}
	if got.Channels[0].Type != "signal" || got.Channels[0].ID != "+15551234567" {
		t.Errorf("channel[0] mismatch: %+v", got.Channels[0])
	}
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
	if err := WriteSessionChannels(cfg); err != nil {
		t.Fatalf("WriteSessionChannels: %v", err)
	}

	p := SessionChannelsPath("agent_newbot", "newsess")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("expected file %q to exist: %v", p, err)
	}
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
	if err := EnsureSessionChannel(agentID, sessionID, chanType, chanID); err != nil {
		t.Fatalf("first EnsureSessionChannel: %v", err)
	}
	cfg, _ := ReadSessionChannels(agentID, sessionID)
	if len(cfg.Channels) != 1 {
		t.Fatalf("expected 1 channel after first ensure, got %d", len(cfg.Channels))
	}

	// Second call should be a no-op.
	if err := EnsureSessionChannel(agentID, sessionID, chanType, chanID); err != nil {
		t.Fatalf("second EnsureSessionChannel: %v", err)
	}
	cfg, _ = ReadSessionChannels(agentID, sessionID)
	if len(cfg.Channels) != 1 {
		t.Fatalf("expected still 1 channel after idempotent ensure, got %d", len(cfg.Channels))
	}
}

// TestEnsureSessionChannel_PreservesExisting verifies adding a new channel
// does not overwrite an existing one.
func TestEnsureSessionChannel_PreservesExisting(t *testing.T) {
	setupStoreDir(t)

	const agentID, sessionID = "agent_multi", "sess-multi"
	if err := EnsureSessionChannel(agentID, sessionID, "signal", "+1111"); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSessionChannel(agentID, sessionID, "slack", "CSLACK1"); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadSessionChannels(agentID, sessionID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(cfg.Channels) != 2 {
		t.Fatalf("expected 2 channels, got %d: %v", len(cfg.Channels), cfg.Channels)
	}
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
		if err := WriteSessionChannels(c); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	found, err := FindAllSessionChannelsConfigs()
	if err != nil {
		t.Fatalf("FindAllSessionChannelsConfigs: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(found))
	}
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
	if err := WriteSessionChannels(valid); err != nil {
		t.Fatal(err)
	}

	// Write a malformed .channels.json file.
	sessDir := filepath.Join(AgentDir("agent_valid"), "sessions")
	badFile := filepath.Join(sessDir, "bad.channels.json")
	if err := os.WriteFile(badFile, []byte("{not valid json}"), 0o600); err != nil {
		t.Fatal(err)
	}

	found, err := FindAllSessionChannelsConfigs()
	if err != nil {
		t.Fatalf("FindAllSessionChannelsConfigs: %v", err)
	}
	// Only the valid one should be returned.
	if len(found) != 1 {
		t.Fatalf("expected 1 valid config, got %d", len(found))
	}
}

// TestFindAllSessionChannelsConfigs_Empty verifies empty data dir returns nil.
func TestFindAllSessionChannelsConfigs_Empty(t *testing.T) {
	setupStoreDir(t)

	found, err := FindAllSessionChannelsConfigs()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("expected 0 configs, got %d", len(found))
	}
}

// TestSessionChannelsPath verifies the path follows the same naming as SessionPath.
func TestSessionChannelsPath(t *testing.T) {
	setupStoreDir(t)

	p := SessionChannelsPath("agent_foo", "sess1")
	sessP := SessionPath("agent_foo", "sess1")

	// channels path should share the same base as the session path.
	if filepath.Dir(p) != filepath.Dir(sessP) {
		t.Errorf("dir mismatch: channels=%q session=%q", filepath.Dir(p), filepath.Dir(sessP))
	}
	// channels path should end with .channels.json.
	if filepath.Ext(p) != ".json" {
		t.Errorf("expected .json extension, got: %s", p)
	}
}
