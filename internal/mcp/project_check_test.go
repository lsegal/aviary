package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/codingskills"
	"github.com/lsegal/aviary/internal/config"
)

// stubProjectCheck replaces gh, PATH lookups, the home directory and the
// configuration for one test. gh answers from responses keyed by the joined
// arguments; a missing key fails the call. The configuration runs jobs on
// claude, codex and opencode until a test sets its own with
// stubProjectConfig.
func stubProjectCheck(t *testing.T, onPath []string, responses map[string]string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	oldGH, oldLook, oldHome, oldConfig := projectGH, projectLookPath, projectHomeDir, projectConfig
	t.Cleanup(func() {
		projectGH, projectLookPath, projectHomeDir, projectConfig = oldGH, oldLook, oldHome, oldConfig
	})
	stubProjectConfig(&config.Config{Models: config.ModelsConfig{Defaults: &config.ModelDefaults{
		Model: "cli/claude", Fallbacks: []string{"cli/codex", "cli/opencode"},
	}}})
	projectGH = func(_ context.Context, args ...string) ([]byte, error) {
		if out, ok := responses[strings.Join(args, " ")]; ok {
			return []byte(out), nil
		}
		return nil, errors.New("HTTP 404: Not Found")
	}
	projectLookPath = func(name string) (string, error) {
		for _, p := range onPath {
			if p == name {
				return "/usr/local/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	projectHomeDir = func() (string, error) { return home, nil }
	return home
}

func stubProjectConfig(cfg *config.Config) {
	projectConfig = func() (*config.Config, error) { return cfg, nil }
}

func installSkill(t *testing.T, home, dir, version string) {
	t.Helper()
	path := filepath.Join(home, dir, "gh-fix", "SKILL.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	front := "---\nname: gh-fix\n"
	if version != "" {
		front += "version: " + version + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(front+"---\n# gh-fix\n"), 0o644))
}

func checkByName(res *ProjectCheckResult) map[string]ProjectCheck {
	out := map[string]ProjectCheck{}
	for _, c := range res.Checks {
		out[c.Name] = c
	}
	return out
}

func TestProjectCheckAllReady(t *testing.T) {
	home := stubProjectCheck(t, []string{"gh", "claude", "codex"}, map[string]string{
		"auth status":              "",
		"api repos/lsegal/zvidlib": `{"full_name":"lsegal/zvidlib","permissions":{"push":false}}`,
	})
	installSkill(t, home, ".claude/skills", "1.6.0")
	installSkill(t, home, ".agents/skills", "1.6.0")

	res, err := runProjectCheck(context.Background(), projectCheckArgs{Target: "https://github.com/lsegal/zvidlib.git"})
	require.NoError(t, err)
	assert.Equal(t, "lsegal/zvidlib", res.Target)
	assert.Equal(t, "repo", res.Kind)
	assert.True(t, res.OK)
	assert.True(t, res.Fork)
	checks := checkByName(res)
	assert.Equal(t, projectCheckOK, checks["github"].Status)
	assert.Equal(t, "Found claude and codex", checks["clis"].Message)
	assert.Equal(t, "gh-fix 1.6.0 installed for all 2", checks["skills"].Message)
	_, hasLabel := checks["ready_label"]
	assert.False(t, hasLabel, "pickup needs no label, so none is checked")
	assert.Equal(t, projectCheckWarn, checks["fork"].Status)
	assert.Len(t, res.CLIs, 2)
	assert.Equal(t, "1.6.0", res.CLIs[1].Skills["gh-fix"])
}

func TestProjectCheckFindsSkillsInCLIAlternateDirs(t *testing.T) {
	home := stubProjectCheck(t, []string{"gh", "codex", "opencode"}, map[string]string{"auth status": ""})
	installSkill(t, home, ".codex/skills", "1.7.0")
	installSkill(t, home, ".config/opencode/skill", "1.7.0")

	res, err := runProjectCheck(context.Background(), projectCheckArgs{Target: "users/lsegal/projects/4"})
	require.NoError(t, err)
	checks := checkByName(res)
	assert.Equal(t, "Found codex and opencode", checks["clis"].Message)
	assert.Equal(t, "gh-fix 1.7.0 installed for all 2", checks["skills"].Message)
}

func TestProjectCheckListsRegistryCLIsWhenNoneFound(t *testing.T) {
	stubProjectCheck(t, []string{"gh"}, map[string]string{"auth status": ""})
	res, err := runProjectCheck(context.Background(), projectCheckArgs{Target: "users/lsegal/projects/4"})
	require.NoError(t, err)
	msg := checkByName(res)["clis"].Message
	for _, cli := range codingskills.Default().CLIs() {
		assert.Contains(t, msg, cli.ID)
	}
}

func TestProjectCheckReportsEachFailure(t *testing.T) {
	stubProjectCheck(t, []string{"gh", "codex"}, map[string]string{
		"auth status":              "",
		"api repos/lsegal/zvidlib": `{"permissions":{"push":true}}`,
	})
	res, err := runProjectCheck(context.Background(), projectCheckArgs{Target: "lsegal/zvidlib"})
	require.NoError(t, err)
	assert.False(t, res.OK)
	assert.False(t, res.Fork)
	checks := checkByName(res)
	assert.Equal(t, projectCheckFail, checks["skills"].Status)
	assert.Equal(t, "gh-fix is not installed for codex", checks["skills"].Message)
	_, hasFork := checks["fork"]
	assert.False(t, hasFork)
}

// The screenshot in #1018: a project running on claude/opus must not need
// attention because agy, gemini and opencode, which it never uses, lack gh-fix.
func TestProjectCheckIgnoresSkillsOfUnusedCLIs(t *testing.T) {
	home := stubProjectCheck(t, []string{"gh", "agy", "claude", "gemini", "opencode"}, map[string]string{
		"auth status":             "",
		"api repos/lsegal/aviary": `{"permissions":{"push":true}}`,
	})
	stubProjectConfig(&config.Config{Agents: []config.AgentConfig{
		{Name: "Aviary", Model: "claude/opus", Projects: config.AgentProjectsConfig{{Target: "lsegal/aviary"}}},
	}})
	installSkill(t, home, ".claude/skills", "1.6.0")

	res, err := runProjectCheck(context.Background(), projectCheckArgs{Target: "lsegal/aviary"})
	require.NoError(t, err)
	assert.True(t, res.OK, "unused CLIs never need attention")
	checks := checkByName(res)
	assert.Equal(t, projectCheckOK, checks["skills"].Status)
	assert.Equal(t, "gh-fix 1.6.0 installed for claude", checks["skills"].Message)
	assert.Equal(t, projectCheckInfo, checks["skills_unused"].Status)
	assert.Equal(t, "gh-fix also missing for agy, gemini and opencode (not used)", checks["skills_unused"].Message)
	for _, cli := range res.CLIs {
		assert.Equal(t, cli.Name == "claude", cli.Used, cli.Name)
	}
}

func TestProjectCheckFailsForUsedCLIsMissingSkills(t *testing.T) {
	home := stubProjectCheck(t, []string{"gh", "claude", "codex", "gemini", "cline"}, map[string]string{"auth status": ""})
	installSkill(t, home, ".claude/skills", "")
	cfg := &config.Config{
		Agents:   []config.AgentConfig{{Name: "coder", Model: "claude/opus", Projects: config.AgentProjectsConfig{{Target: "users/lsegal/projects/4"}}}},
		Projects: config.ProjectsConfig{ChatModel: "claude/opus"},
	}
	stubProjectConfig(cfg)
	check := func(args projectCheckArgs) ProjectCheck {
		t.Helper()
		args.Target = "users/lsegal/projects/4"
		res, err := runProjectCheck(context.Background(), args)
		require.NoError(t, err)
		assert.Equal(t, checkByName(res)["skills"].Status != projectCheckFail, res.OK)
		return checkByName(res)["skills"]
	}

	assert.Equal(t, projectCheckOK, check(projectCheckArgs{}).Status)
	assert.Equal(t, "gh-fix is not installed for codex", check(projectCheckArgs{Models: []string{"codex/gpt-5.6-terra"}}).Message,
		"an unsaved primary model is checked")
	assert.Equal(t, "gh-fix is not installed for gemini", check(projectCheckArgs{Agent: "coder", Models: []string{"claude/opus", "cli/gemini"}}).Message,
		"an agent fallback is checked")

	cfg.Projects.ChatFallbacks = []string{"cline/m"}
	assert.Equal(t, "gh-fix is not installed for cline", check(projectCheckArgs{}).Message, "a chat fallback is checked")
	cfg.Projects.ChatFallbacks = nil
	cfg.Projects.UnblockAgent = "codex"
	assert.Equal(t, "gh-fix is not installed for codex", check(projectCheckArgs{}).Message, "the unblock agent is checked")
	cfg.Projects.UnblockAgent = ""
	cfg.Agents[0].Tasks = []config.TaskConfig{{Name: "nightly", Project: "users/lsegal/projects/4", Model: "gemini/m"}}
	assert.Equal(t, "gh-fix is not installed for gemini", check(projectCheckArgs{}).Message, "a scheduled project task is checked")
}

func TestProjectCheckSignedOutAndNoCLIs(t *testing.T) {
	stubProjectCheck(t, []string{"gh"}, nil)
	res, err := runProjectCheck(context.Background(), projectCheckArgs{Target: "lsegal/zvidlib"})
	require.NoError(t, err)
	assert.False(t, res.OK)
	checks := checkByName(res)
	assert.Equal(t, "gh is not signed in; run gh auth login", checks["github"].Message)
	assert.Equal(t, projectCheckFail, checks["clis"].Status)
	assert.Equal(t, projectCheckFail, checks["skills"].Status)

	stubProjectCheck(t, nil, nil)
	res, err = runProjectCheck(context.Background(), projectCheckArgs{Target: "lsegal/zvidlib"})
	require.NoError(t, err)
	assert.Contains(t, checkByName(res)["github"].Message, "gh is not installed")
}

func TestProjectCheckUnreadableAndArchivedRepo(t *testing.T) {
	stubProjectCheck(t, []string{"gh"}, map[string]string{"auth status": ""})
	res, err := runProjectCheck(context.Background(), projectCheckArgs{Target: "lsegal/private"})
	require.NoError(t, err)
	assert.Contains(t, checkByName(res)["github"].Message, "cannot read lsegal/private")

	stubProjectCheck(t, []string{"gh"}, map[string]string{
		"auth status":          "",
		"api repos/lsegal/old": `{"archived":true,"permissions":{"push":true}}`,
	})
	res, err = runProjectCheck(context.Background(), projectCheckArgs{Target: "lsegal/old"})
	require.NoError(t, err)
	assert.Contains(t, checkByName(res)["github"].Message, "archived")
}

func TestParseProjectTarget(t *testing.T) {
	cases := []struct{ in, target, kind, repo string }{
		{"lsegal/aviary", "lsegal/aviary", "repo", "lsegal/aviary"},
		{" https://github.com/lsegal/aviary/ ", "lsegal/aviary", "repo", "lsegal/aviary"},
		{"orgs/acme/projects/12", "orgs/acme/projects/12", "board", ""},
		{"https://github.com/users/lsegal/projects/4", "users/lsegal/projects/4", "board", ""},
		{"lsegal/aviary/discussions", "lsegal/aviary/discussions", "discussions", "lsegal/aviary"},
	}
	for _, c := range cases {
		target, kind, repo, err := parseProjectTarget(c.in)
		require.NoError(t, err, c.in)
		assert.Equal(t, []string{c.target, c.kind, c.repo}, []string{target, kind, repo}, c.in)
	}
	for _, bad := range []string{"", "aviary", "https://example.com/a/b", "a/b/c/d"} {
		_, _, _, err := parseProjectTarget(bad)
		assert.Error(t, err, bad)
	}
}

func TestProjectCheckToolOverMCP(t *testing.T) {
	stubProjectCheck(t, []string{"gh"}, map[string]string{"auth status": ""})
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test"}, nil)
	registerProjectCheckTool(s)
	ct, st := sdkmcp.NewInMemoryTransports()
	_, err := s.Connect(context.Background(), st, nil)
	require.NoError(t, err)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "c"}, nil)
	sess, err := client.Connect(context.Background(), ct, nil)
	require.NoError(t, err)
	defer func() { _ = sess.Close() }()

	tools, err := sess.ListTools(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)
	assert.True(t, tools.Tools[0].Annotations.ReadOnlyHint)

	out, err := sess.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "project_check", Arguments: map[string]any{"target": "users/lsegal/projects/4"},
	})
	require.NoError(t, err)
	require.False(t, out.IsError)
	var res ProjectCheckResult
	require.NoError(t, json.Unmarshal([]byte(out.Content[0].(*sdkmcp.TextContent).Text), &res))
	assert.Equal(t, "board", res.Kind)

	out, err = sess.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "project_check", Arguments: map[string]any{"target": "nope"},
	})
	require.NoError(t, err)
	assert.True(t, out.IsError)
}
