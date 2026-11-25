package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTaskTarget(t *testing.T) {
	cases := []struct {
		in   string
		want TaskTarget
	}{
		{"", TaskTarget{Kind: TargetSilent}},
		{" silent ", TaskTarget{Kind: TargetSilent}},
		{"reply", TaskTarget{Kind: TargetReply}},
		{"session:main", TaskTarget{Kind: TargetSession, Session: "main"}},
		{"channel:Team Slack", TaskTarget{Kind: TargetChannel, Channel: "Team Slack"}},
		{"codeloop:owner/repo#123", TaskTarget{Kind: TargetCodeLoop, Project: "owner/repo", Number: 123}},
		{"codeloop:owner/repo#12?resume", TaskTarget{Kind: TargetCodeLoop, Project: "owner/repo", Number: 12, Resume: true}},
		{"codeloop:owner/repo#12?reopen", TaskTarget{Kind: TargetCodeLoop, Project: "owner/repo", Number: 12, Reopen: true}},
		{"codeloop:owner/repo#12?resume,reopen", TaskTarget{Kind: TargetCodeLoop, Project: "owner/repo", Number: 12, Resume: true, Reopen: true}},
		{"notify", TaskTarget{Kind: TargetNotify}},
		{"notify?failures", TaskTarget{Kind: TargetNotify, FailuresOnly: true}},
		{"codeloop:https://github.com/users/o/projects/3#7", TaskTarget{Kind: TargetCodeLoop, Project: "https://github.com/users/o/projects/3", Number: 7}},
	}
	for _, c := range cases {
		got, err := ParseTaskTarget(c.in)
		require.NoError(t, err, c.in)
		assert.Equal(t, c.want, got, c.in)
		if c.want.Kind != TargetSilent {
			assert.Equal(t, c.in, got.String())
		}
	}
	for _, bad := range []string{"telegram", "session:", "codeloop:", "codeloop:owner/repo", "codeloop:#12", "codeloop:owner/repo#0", "codeloop:owner/repo#x", "codeloop:own er/repo#1", "codeloop:owner/repo#1?", "codeloop:owner/repo#1?wake", "codeloop:owner/repo#1?resume,", "notify?", "notify?success", "notify:x"} {
		_, err := ParseTaskTarget(bad)
		assert.Error(t, err, bad)
	}
}

func TestValidateCodeLoopTaskTarget(t *testing.T) {
	cfg := &Config{Agents: []AgentConfig{{
		Name:     "bot",
		Projects: []ProjectConfig{{Target: "owner/repo"}},
		Tasks: []TaskConfig{
			{Name: "ok", Schedule: "0 * * * *", Prompt: "x", Target: "codeloop:owner/repo#12"},
			{Name: "unknown", Schedule: "0 * * * *", Prompt: "x", Target: "codeloop:other/repo#3"},
			{Name: "malformed", Schedule: "0 * * * *", Prompt: "x", Target: "codeloop:owner/repo"},
			{Name: "options", Schedule: "0 * * * *", Prompt: "x", Target: "codeloop:owner/repo#12?resume,reopen"},
			{Name: "bad-option", Schedule: "0 * * * *", Prompt: "x", Target: "codeloop:owner/repo#12?wake"},
		},
	}}}
	issues := Validate(cfg, nil)
	assert.False(t, hasIssue(issues, `"codeloop:owner/repo#12"`))
	assert.False(t, hasIssue(issues, "codeloop:owner/repo#12?resume,reopen"))
	assert.True(t, hasIssue(issues, `has unknown option "wake"`))
	assert.True(t, hasIssue(issues, `no agent has a Code Loop project "other/repo"`))
	assert.True(t, hasIssue(issues, `codeloop target "codeloop:owner/repo" names no job`))
}
