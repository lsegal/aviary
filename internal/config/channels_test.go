package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelEffectiveNameAndSessionName(t *testing.T) {
	assert.Equal(t, "Team", ChannelConfig{Type: "slack", ID: "ws", Name: " Team "}.EffectiveName())
	assert.Equal(t, "ws", ChannelConfig{Type: "slack", ID: "ws"}.EffectiveName())
	assert.Equal(t, "slack", ChannelConfig{Type: "slack"}.EffectiveName())
	// Keyed by type and ID, so a rename keeps the session.
	assert.Equal(t, "channel:slack:ws", ChannelConfig{Type: "slack", ID: "ws", Name: "Team"}.SessionName())
}

func TestChannelTargetName(t *testing.T) {
	name, ok := ChannelTargetName(" channel:Team Slack ")
	assert.True(t, ok)
	assert.Equal(t, "Team Slack", name)
	_, ok = ChannelTargetName("session:main")
	assert.False(t, ok)
	assert.Equal(t, "channel:Team", ChannelTarget(" Team "))
}

func TestChannelForSession(t *testing.T) {
	a := AgentConfig{Name: "bot", Channels: []ChannelConfig{
		{Type: "slack", ID: "ws", Name: "Team"},
		{Type: "signal", ID: "+1"},
		{Type: "signal", ID: "+2"},
	}}
	assert.Equal(t, 0, a.ChannelForSession("channel:slack:ws", "", ""), "own session")
	assert.Equal(t, 0, a.ChannelForSession("slack:C1", "", ""), "the only Slack channel's conversation")
	assert.Equal(t, -1, a.ChannelForSession("signal:+9", "", ""), "two Signal channels are ambiguous")
	assert.Equal(t, 2, a.ChannelForSession("signal:+9", "signal", "+2"), "the sidecar's configured ID decides")
	assert.Equal(t, -1, a.ChannelForSession("main", "", ""))
	assert.Equal(t, -1, a.ChannelForSession("notes:today", "", ""), "not a channel type")
	assert.Equal(t, 1, a.ChannelByName("+1"))
	assert.Equal(t, 0, a.ChannelByName("team"))
}

func TestValidateChannelNamesAndTargets(t *testing.T) {
	cfg := &Config{Agents: []AgentConfig{{
		Name:  "bot",
		Model: "anthropic/claude-sonnet-4-5",
		Channels: []ChannelConfig{
			{Type: "slack", ID: "a", Name: "Team"},
			{Type: "discord", ID: "b", Name: "team"},
		},
		Tasks: []TaskConfig{
			{Name: "ok", Schedule: "0 * * * *", Prompt: "x", Target: "channel:Team"},
			{Name: "missing", Schedule: "0 * * * *", Prompt: "x", Target: "channel:Nope"},
			{Name: "legacy", Schedule: "0 * * * *", Prompt: "x", Target: "slack:a:C1"},
		},
	}}}
	issues := Validate(cfg, nil)
	assert.True(t, hasIssue(issues, `duplicate channel name "team"`))
	assert.True(t, hasIssue(issues, `agent "bot" has no channel named "Nope"`))
	assert.True(t, hasIssue(issues, `invalid target "slack:a:C1"`))
	assert.False(t, hasIssue(issues, `no channel named "Team"`))
}

func TestLoadMigratesChannelTargets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AVIARY_CONFIG_BASE_DIR", "")
	path := filepath.Join(dir, "aviary.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`agents:
  - name: bot
    channels:
      - type: slack
        id: ws
        name: Team
      - type: signal
        id: "+1555"
    tasks:
      - name: by-id
        schedule: "0 * * * *"
        prompt: x
        target: slack:ws:C123
      - name: by-session
        schedule: "0 * * * *"
        prompt: x
        target: session:signal:+1999
      - name: own-session
        schedule: "0 * * * *"
        prompt: x
        target: session:channel:slack:ws
      - name: plain-session
        schedule: "0 * * * *"
        prompt: x
        target: session:notes
    projects:
      - target: o/r
        reports:
          agent: bot
          targets: [slack:ws:C999, "session:main"]
`), 0o640))

	cfg, err := Load(path)
	require.NoError(t, err)
	a := cfg.Agents[0]
	targets := map[string]string{}
	for _, task := range a.Tasks {
		targets[task.Name] = task.Target
	}
	assert.Equal(t, "channel:Team", targets["by-id"])
	assert.Equal(t, "channel:+1555", targets["by-session"])
	assert.Equal(t, "channel:Team", targets["own-session"])
	assert.Equal(t, "session:notes", targets["plain-session"])
	assert.Equal(t, []string{"channel:Team", "session:main"}, a.Projects[0].Reports.Targets)
	// A channel with no conversation of its own takes the first target's.
	assert.Equal(t, "C123", a.Channels[0].DeliverTo)
	assert.Equal(t, "+1999", a.Channels[1].DeliverTo)
}
