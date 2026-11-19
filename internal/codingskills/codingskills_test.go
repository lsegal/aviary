package codingskills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

var testBundle = fstest.MapFS{
	".agents/skills/gh-fix/SKILL.md":               {Data: []byte("fix v2\n")},
	".agents/skills/gh-fix/agents/openai.yaml":     {Data: []byte("fix agent\n")},
	".agents/skills/gh-discuss/SKILL.md":           {Data: []byte("discuss v2\n")},
	".agents/skills/gh-discuss/agents/openai.yaml": {Data: []byte("discuss agent\n")},
	".agents/skills/gh-create/SKILL.md":            {Data: []byte("create v2\n")},
	".agents/skills/gh-create/agents/openai.yaml":  {Data: []byte("create agent\n")},
}

var testCLIs = []CLI{
	{ID: "claude", Name: "Claude", Target: "claude-code", Binary: "claude"},
	{ID: "codex", Name: "Codex", Target: "codex", Binary: "codex"},
	{ID: "gemini", Name: "Gemini", Target: "gemini-cli", Binary: "gemini"},
}

type fakeNpx struct {
	calls [][]string
	err   error
	// onRun inspects the host while npx would run.
	onRun func(args []string)
}

func (f *fakeNpx) run(_ context.Context, args ...string) error {
	f.calls = append(f.calls, args)
	if f.onRun != nil {
		f.onRun(args)
	}
	return f.err
}

func newTestManager(t *testing.T, version string, onPath ...string) (*Manager, *fakeNpx, string) {
	t.Helper()
	home := t.TempDir()
	npx := &fakeNpx{}
	m := &Manager{
		Env: Env{
			Home:   home,
			Getenv: func(string) string { return "" },
			LookPath: func(binary string) (string, error) {
				for _, name := range onPath {
					if name == binary {
						return "/usr/bin/" + binary, nil
					}
				}
				return "", errors.New("not found")
			},
		},
		Bundle:  testBundle,
		Version: func() string { return version },
		CLIs:    func() []CLI { return testCLIs },
		Run:     npx.run,
	}
	return m, npx, home
}

func writeSkill(t *testing.T, dir, name, skill string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, name, "agents"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, name, "SKILL.md"), []byte(skill), 0o644))
	agent := strings.TrimPrefix(name, "gh-") + " agent\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, name, "agents", "openai.yaml"), []byte(agent), 0o644))
}

func TestSkillsDirFollowsSkillsShLayout(t *testing.T) {
	env := Env{Home: "/home/u", Getenv: func(string) string { return "" }}
	assert.Equal(t, filepath.Join("/home/u", ".claude", "skills"), env.SkillsDir("claude-code"))
	assert.Equal(t, filepath.Join("/home/u", ".agents", "skills"), env.SkillsDir("codex"))
	assert.Equal(t, filepath.Join("/home/u", ".agents", "skills"), env.SkillsDir("cline"))
	assert.Equal(t, filepath.Join("/home/u", ".agents", "skills"), env.SkillsDir("universal"))
	assert.Equal(t, filepath.Join("/home/u", ".gemini", "skills"), env.SkillsDir("gemini-cli"))
	assert.Equal(t, filepath.Join("/home/u", ".gemini", "antigravity-cli", "skills"), env.SkillsDir("antigravity-cli"))
	assert.Equal(t, filepath.Join("/home/u", ".config", "opencode", "skills"), env.SkillsDir("opencode"))

	env.Getenv = func(key string) string {
		return map[string]string{"CLAUDE_CONFIG_DIR": "/cfg/claude", "XDG_CONFIG_HOME": "/xdg"}[key]
	}
	assert.Equal(t, filepath.Join("/cfg/claude", "skills"), env.SkillsDir("claude-code"))
	assert.Equal(t, filepath.Join("/xdg", "opencode", "skills"), env.SkillsDir("opencode"))
}

func TestStatusReportsVersionPerSkillPerCLI(t *testing.T) {
	m, _, home := newTestManager(t, "v2.0.0", "claude", "codex")
	canonical := filepath.Join(home, ".agents", "skills")
	writeSkill(t, canonical, "gh-fix", "fix v2\r\n") // CRLF still matches
	writeSkill(t, canonical, "gh-discuss", "discuss v1\n")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".agents", ".skill-lock.json"), []byte(`{
		"version": 3,
		"skills": {
			"gh-fix": {"source": "lsegal/aviary", "ref": "v2.0.0"},
			"gh-discuss": {"source": "lsegal/aviary", "ref": "v1.5.16"}
		}
	}`), 0o644))
	claudeDir := filepath.Join(home, ".claude", "skills")
	require.NoError(t, os.MkdirAll(claudeDir, 0o755))
	if err := os.Symlink(filepath.Join(canonical, "gh-fix"), filepath.Join(claudeDir, "gh-fix")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// A copy outside the canonical directory has no lock entry to date it.
	writeSkill(t, claudeDir, "gh-discuss", "discuss v0\n")

	status := m.Status()

	assert.Equal(t, "2.0.0", status.Version)
	assert.True(t, status.Release)
	assert.Equal(t, Repo, status.Source)
	require.Len(t, status.CLIs, 2, "only CLIs on PATH are listed")
	assert.Equal(t, "claude", status.CLIs[0].ID)
	assert.Equal(t, "codex", status.CLIs[1].ID)
	require.Len(t, status.Skills, 3)

	fix := status.Skills[0]
	assert.Equal(t, "gh-fix", fix.Name)
	assert.True(t, fix.RunByProjects)
	assert.Equal(t, Install{CLI: "claude", Installed: true, Current: true, Version: "2.0.0", Source: Repo, Path: filepath.Join(claudeDir, "gh-fix")}, fix.Installs[0])
	assert.Equal(t, Install{CLI: "codex", Installed: true, Current: true, Version: "2.0.0", Source: Repo, Path: filepath.Join(canonical, "gh-fix")}, fix.Installs[1])

	discuss := status.Skills[1]
	assert.Equal(t, Install{CLI: "claude", Installed: true, Path: filepath.Join(claudeDir, "gh-discuss")}, discuss.Installs[0])
	assert.Equal(t, Install{CLI: "codex", Installed: true, Version: "1.5.16", Source: Repo, Path: filepath.Join(canonical, "gh-discuss")}, discuss.Installs[1])

	create := status.Skills[2]
	assert.False(t, create.RunByProjects)
	assert.Equal(t, []Install{{CLI: "claude"}, {CLI: "codex"}}, create.Installs)

	assert.Equal(t, 2, status.Outdated)
}

func TestStatusIgnoresLockFromAnotherSource(t *testing.T) {
	m, _, home := newTestManager(t, "v2.0.0", "codex")
	canonical := filepath.Join(home, ".agents", "skills")
	writeSkill(t, canonical, "gh-fix", "someone else's fix\n")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".agents", ".skill-lock.json"),
		[]byte(`{"skills":{"gh-fix":{"source":"other/repo","ref":"v9.9.9"}}}`), 0o644))

	install := m.Status().Skills[0].Installs[0]
	assert.True(t, install.Installed)
	assert.False(t, install.Current)
	assert.Empty(t, install.Version)
	assert.Equal(t, "other/repo", install.Source)
}

func TestStatusOnDevBuild(t *testing.T) {
	m, _, home := newTestManager(t, "dev", "codex")
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "gh-fix", "fix v2\n")

	status := m.Status()
	assert.Equal(t, "dev", status.Version)
	assert.False(t, status.Release)
	assert.Equal(t, "dev", status.Skills[0].Installs[0].Version)
}

func TestInstallFromReleaseTag(t *testing.T) {
	m, npx, home := newTestManager(t, "2.0.0", "claude", "codex", "gemini")
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "gh-fix", "fix v2\n")
	writeSkill(t, filepath.Join(home, ".gemini", "skills"), "gh-discuss", "discuss v1\n")

	result, err := m.Install(context.Background(), Request{Missing: true, Outdated: true})
	require.NoError(t, err)

	tree := "https://github.com/lsegal/aviary/tree/v2.0.0/.agents/skills/"
	assert.Equal(t, [][]string{
		// codex already has the current gh-fix.
		{"--yes", "skills", "add", tree + "gh-fix", "--global", "--agent", "claude-code", "--agent", "gemini-cli", "-y"},
		{"--yes", "skills", "add", tree + "gh-discuss", "--global", "--agent", "claude-code", "--agent", "codex", "--agent", "gemini-cli", "-y"},
		{"--yes", "skills", "add", tree + "gh-create", "--global", "--agent", "claude-code", "--agent", "codex", "--agent", "gemini-cli", "-y"},
	}, npx.calls)
	assert.Equal(t, []string{
		"gh-fix/claude", "gh-fix/gemini",
		"gh-discuss/claude", "gh-discuss/codex", "gh-discuss/gemini",
		"gh-create/claude", "gh-create/codex", "gh-create/gemini",
	}, result.Installed)
}

func TestInstallUpdateOnlySkipsMissing(t *testing.T) {
	m, npx, home := newTestManager(t, "2.0.0", "claude", "gemini")
	writeSkill(t, filepath.Join(home, ".gemini", "skills"), "gh-discuss", "discuss v1\n")
	writeSkill(t, filepath.Join(home, ".claude", "skills"), "gh-create", "create v2\n")

	result, err := m.Install(context.Background(), Request{Outdated: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"gh-discuss/gemini"}, result.Installed)
	require.Len(t, npx.calls, 1)
	assert.Contains(t, npx.calls[0], "https://github.com/lsegal/aviary/tree/v2.0.0/.agents/skills/gh-discuss")
}

func TestInstallOneSkillForOneCLI(t *testing.T) {
	m, npx, _ := newTestManager(t, "2.0.0", "claude", "codex")

	result, err := m.Install(context.Background(), Request{Skill: "gh-create", CLI: "codex", Missing: true, Outdated: true})
	require.NoError(t, err)
	assert.Equal(t, []string{"gh-create/codex"}, result.Installed)
	assert.Equal(t, [][]string{{"--yes", "skills", "add", "https://github.com/lsegal/aviary/tree/v2.0.0/.agents/skills/gh-create", "--global", "--agent", "codex", "-y"}}, npx.calls)
}

func TestInstallRejectsUnknownSkillOrCLI(t *testing.T) {
	m, npx, _ := newTestManager(t, "2.0.0", "claude")

	_, err := m.Install(context.Background(), Request{Skill: "commit", Missing: true})
	assert.ErrorContains(t, err, `unknown coding-CLI skill "commit"`)
	_, err = m.Install(context.Background(), Request{CLI: "gemini", Missing: true})
	assert.ErrorContains(t, err, `coding CLI "gemini" is not installed`)
	assert.Empty(t, npx.calls)
}

func TestInstallReportsNpxFailure(t *testing.T) {
	m, npx, _ := newTestManager(t, "2.0.0", "claude")
	npx.err = errors.New("exit status 1: clone failed")

	result, err := m.Install(context.Background(), Request{Missing: true})
	assert.ErrorContains(t, err, "install gh-fix: exit status 1: clone failed")
	assert.Empty(t, result.Installed)
	assert.Len(t, npx.calls, 1, "stops at the first failure")
}

func TestInstallOnDevBuildUsesBundledCopy(t *testing.T) {
	m, npx, _ := newTestManager(t, "dev", "claude")
	var sources []string
	npx.onRun = func(args []string) {
		source := args[3]
		sources = append(sources, source)
		data, err := os.ReadFile(filepath.Join(source, "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, strings.TrimPrefix(filepath.Base(source), "gh-")+" v2\n", string(data))
	}

	_, err := m.Install(context.Background(), Request{Missing: true})
	require.NoError(t, err)
	require.Len(t, sources, 3)
	_, err = os.Stat(sources[0])
	assert.True(t, os.IsNotExist(err), "the extracted copy is removed afterwards")
}

func TestSyncFollowsConfig(t *testing.T) {
	m, npx, home := newTestManager(t, "2.0.0", "claude")
	claudeDir := filepath.Join(home, ".claude", "skills")
	writeSkill(t, claudeDir, "gh-fix", "fix v1\n")
	writeSkill(t, claudeDir, "gh-discuss", "discuss v2\n")
	off := false

	m.Sync(context.Background(), &config.Config{CodingSkills: &config.CodingSkillsConfig{AutoUpdate: &off, InstallNewCLIs: &off}})
	assert.Empty(t, npx.calls)

	m.Sync(context.Background(), &config.Config{CodingSkills: &config.CodingSkillsConfig{InstallNewCLIs: &off}})
	require.Len(t, npx.calls, 1)
	assert.Contains(t, npx.calls[0][3], "gh-fix")

	npx.calls = nil
	m.Sync(context.Background(), &config.Config{CodingSkills: &config.CodingSkillsConfig{AutoUpdate: &off}})
	require.Len(t, npx.calls, 1)
	assert.Contains(t, npx.calls[0][3], "gh-create")

	npx.calls = nil
	m.Sync(context.Background(), &config.Config{})
	assert.Len(t, npx.calls, 2, "defaults update and install")
}

func TestSyncSkipsDevBuilds(t *testing.T) {
	m, npx, _ := newTestManager(t, "dev", "claude")
	m.Sync(context.Background(), &config.Config{})
	assert.Empty(t, npx.calls)
}
