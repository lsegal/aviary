package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/skills"
)

func TestConfigureSkillsSummary(t *testing.T) {
	cfg := config.Default()
	got := configureSkillsSummary(&cfg)
	assert.Equal(t, "No skills enabled", got)

	cfg.Skills = map[string]config.SkillConfig{
		"gogcli": {Enabled: true},
	}
	got = configureSkillsSummary(&cfg)
	assert.Equal(t, "1 skill enabled", got)

}

func TestSkillMgrSaveCurrentPersistsConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AVIARY_CONFIG_BASE_DIR", filepath.Join(tmp, "base"))

	cfg := config.Default()
	cfgPath := filepath.Join(tmp, "aviary.yaml")
	m := newSkillMgrModel(&cfg, cfgPath)
	m.refreshInstalled()
	assert.NotEqual(t, 0, len(m.installed))
	found := false
	for i, sk := range m.installed {
		if sk.Name == "gogcli" {
			m.cursor = i
			found = true
			break
		}
	}
	assert.True(t, found)

	gotModel, _ := m.toggleEnabled()
	m = gotModel.(skillMgrModel)

	sk := m.cfg.Skills["gogcli"]
	assert.True(t, sk.Enabled)
	assert.Nil(t, sk.Settings)

	loaded, err := config.Load(cfgPath)
	assert.NoError(t, err)
	assert.True(t, loaded.Skills["gogcli"].Enabled)

}

// newSkillMgrTestModel returns a skill manager backed by a temp config path and
// temp skill directories holding the given on-disk skills (name to SKILL.md).
func newSkillMgrTestModel(t *testing.T, disk map[string]string) (skillMgrModel, string) {
	t.Helper()
	tmp := t.TempDir()
	base := filepath.Join(tmp, "base")
	home := filepath.Join(tmp, "home")
	t.Setenv("AVIARY_CONFIG_BASE_DIR", base)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for name, body := range disk {
		dir := filepath.Join(base, "skills", name)
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600))
	}
	cfg := config.Default()
	cfgPath := filepath.Join(tmp, "aviary.yaml")
	return newSkillMgrModel(&cfg, cfgPath), cfgPath
}

func skillMgrKey(t *testing.T, m skillMgrModel, key tea.KeyMsg) (skillMgrModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(key)
	got, ok := next.(skillMgrModel)
	require.True(t, ok)
	return got, cmd
}

func skillMgrRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func skillMgrIndex(t *testing.T, m skillMgrModel, name string) int {
	t.Helper()
	for i, sk := range m.installed {
		if sk.Name == name {
			return i
		}
	}
	t.Fatalf("skill %q not installed", name)
	return -1
}

func assertSkillMgrQuit(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	require.NotNil(t, cmd)
	assert.IsType(t, tea.QuitMsg{}, cmd())
}

func TestSkillMgrInitDoesNotQuit(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, nil)
	assert.Nil(t, m.Init())
}

func TestSkillMgrUpdateIgnoresNonKeyMessages(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, nil)
	m.refreshInstalled()
	m.cursor = 1
	got, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	assert.Nil(t, cmd)
	assert.Equal(t, m, got)
}

func TestSkillMgrListNavigation(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, nil)
	m.refreshInstalled()
	require.GreaterOrEqual(t, len(m.installed), 2)
	last := len(m.installed) - 1

	// Up at the top stays put.
	for _, key := range []tea.KeyMsg{{Type: tea.KeyUp}, skillMgrRunes("k"), {Type: tea.KeyShiftTab}} {
		var cmd tea.Cmd
		m, cmd = skillMgrKey(t, m, key)
		assert.Nil(t, cmd)
		assert.Equal(t, 0, m.cursor)
	}

	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, m.cursor)
	m, _ = skillMgrKey(t, m, skillMgrRunes("k"))
	assert.Equal(t, 0, m.cursor)
	m, _ = skillMgrKey(t, m, skillMgrRunes("j"))
	assert.Equal(t, 1, m.cursor)
	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, 0, m.cursor)

	// Down at the bottom stays put.
	for range last + 2 {
		m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	}
	assert.Equal(t, last, m.cursor)
	assert.Equal(t, skillModeList, m.mode)
}

func TestSkillMgrListQuitKeys(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, nil)
	for _, key := range []tea.KeyMsg{{Type: tea.KeyCtrlC}, skillMgrRunes("q"), {Type: tea.KeyEsc}} {
		_, cmd := skillMgrKey(t, m, key)
		assertSkillMgrQuit(t, cmd)
	}
}

func TestSkillMgrListEnterOpensEdit(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, nil)

	// With no skills loaded, Enter does nothing.
	got, cmd := skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd)
	assert.Equal(t, skillModeList, got.mode)

	m.refreshInstalled()
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeySpace}} {
		m.mode = skillModeList
		m.editCursor = 1
		got, cmd := skillMgrKey(t, m, key)
		assert.Nil(t, cmd)
		assert.Equal(t, skillModeEdit, got.mode)
		assert.Equal(t, 0, got.editCursor)
	}
}

func TestSkillMgrEditNavigationAndBack(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, nil)
	m.refreshInstalled()
	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, skillModeEdit, m.mode)

	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 0, m.editCursor)
	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, 1, m.editCursor)
	m, _ = skillMgrKey(t, m, skillMgrRunes("j"))
	assert.Equal(t, 1, m.editCursor)
	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, m.editCursor)
	m, _ = skillMgrKey(t, m, skillMgrRunes("k"))
	assert.Equal(t, 0, m.editCursor)
	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, 0, m.editCursor)

	// Unbound keys are ignored.
	got, cmd := skillMgrKey(t, m, skillMgrRunes("x"))
	assert.Nil(t, cmd)
	assert.Equal(t, m, got)

	// Escape returns to the list without quitting.
	m, cmd = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	assert.Nil(t, cmd)
	assert.Equal(t, skillModeList, m.mode)

	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
	assertSkillMgrQuit(t, cmd)
}

func TestSkillMgrEditToggleAndSavePersist(t *testing.T) {
	m, cfgPath := newSkillMgrTestModel(t, map[string]string{
		"alpha": "---\nname: alpha\ndescription: Alpha skill\n---\nBody\n",
	})
	m.cfg.Skills = nil
	m.refreshInstalled()
	m.cursor = skillMgrIndex(t, m, "alpha")
	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	// Enter on the Enabled row toggles and saves.
	m, cmd := skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd)
	assert.True(t, m.cfg.Skills["alpha"].Enabled)
	assert.Equal(t, "Saved alpha.", m.message)
	assert.Empty(t, m.err)
	assert.True(t, m.installed[m.cursor].Enabled)
	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.True(t, loaded.Skills["alpha"].Enabled)

	// Space toggles it back off.
	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeySpace})
	assert.False(t, m.cfg.Skills["alpha"].Enabled)
	loaded, err = config.Load(cfgPath)
	require.NoError(t, err)
	assert.False(t, loaded.Skills["alpha"].Enabled)

	// Enter on the Save row saves without toggling.
	m.message = ""
	m, _ = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m, cmd = skillMgrKey(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd)
	assert.Equal(t, "Saved alpha.", m.message)
	assert.False(t, m.cfg.Skills["alpha"].Enabled)
	assert.Equal(t, skillModeEdit, m.mode)
}

func TestSkillMgrSaveCurrentCreatesSkillsMap(t *testing.T) {
	m, cfgPath := newSkillMgrTestModel(t, map[string]string{"alpha": "Body\n"})
	m.cfg.Skills = nil
	m.refreshInstalled()
	m.cursor = skillMgrIndex(t, m, "alpha")

	got, cmd := m.saveCurrent()
	assert.Nil(t, cmd)
	m = got.(skillMgrModel)
	assert.Equal(t, "Saved alpha.", m.message)
	assert.Empty(t, m.err)
	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.False(t, loaded.Skills["alpha"].Enabled)
}

func TestSkillMgrSaveCurrentError(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, map[string]string{"alpha": "Body\n"})
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	m.cfgPath = filepath.Join(blocker, "aviary.yaml")
	m.refreshInstalled()
	m.cursor = skillMgrIndex(t, m, "alpha")
	m.message = "stale"

	got, cmd := m.toggleEnabled()
	assert.Nil(t, cmd)
	m = got.(skillMgrModel)
	assert.Contains(t, m.err, "creating config dir")
	assert.Equal(t, "stale", m.message)
	assert.Contains(t, m.View(), "creating config dir")
}

func TestSkillMgrNoCurrentSkill(t *testing.T) {
	m, cfgPath := newSkillMgrTestModel(t, nil)
	for _, cursor := range []int{-1, 0} {
		m.cursor = cursor
		assert.Empty(t, m.currentSkillName())

		got, cmd := m.toggleEnabled()
		assert.Nil(t, cmd)
		assert.Empty(t, got.(skillMgrModel).message)

		got, cmd = m.saveCurrent()
		assert.Nil(t, cmd)
		assert.Empty(t, got.(skillMgrModel).message)
	}
	assert.Nil(t, m.cfg.Skills["alpha"].Settings)
	assert.NoFileExists(t, cfgPath)
}

func TestSkillMgrRefreshInstalledClampsCursor(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, nil)
	m.cursor = 1000
	m.refreshInstalled()
	require.NotEmpty(t, m.installed)
	assert.Equal(t, len(m.installed)-1, m.cursor)
	assert.Empty(t, m.err)
}

func TestSkillMgrRefreshInstalledError(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, map[string]string{"broken": "Body\n"})
	manifest := filepath.Join(os.Getenv("AVIARY_CONFIG_BASE_DIR"), "skills", "broken", "aviary-skill.json")
	require.NoError(t, os.WriteFile(manifest, []byte("{not json"), 0o600))

	m.refreshInstalled()
	assert.NotEmpty(t, m.err)
	assert.Empty(t, m.installed)
}

func TestSkillMgrViewList(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, map[string]string{
		"alpha": "---\nname: alpha\ndescription: Alpha skill\n---\nBody\n",
		"beta":  "Body\n",
	})

	view := m.View()
	assert.Contains(t, view, "Skills")
	assert.Contains(t, view, "Enable installed skills.")
	assert.Contains(t, view, "No installed skills found.")
	assert.Contains(t, view, "Enter edit/select · Tab/↑/↓ navigate · Esc/q quit")

	m.cfg.Skills = map[string]config.SkillConfig{"alpha": {Enabled: true}}
	m.refreshInstalled()
	m.cursor = skillMgrIndex(t, m, "alpha")
	m.message = "Saved alpha."
	view = m.View()
	assert.NotContains(t, view, "No installed skills found.")
	assert.Contains(t, view, "Alpha skill")
	assert.Contains(t, view, "Saved alpha.")
	lines := strings.Split(view, "\n")
	var alphaLine, betaLine string
	for _, line := range lines {
		switch {
		case strings.Contains(line, "alpha "):
			alphaLine = line
		case strings.Contains(line, "beta "):
			betaLine = line
		}
	}
	assert.Contains(t, alphaLine, ">")
	assert.Contains(t, alphaLine, "enabled")
	assert.Contains(t, alphaLine, "disk")
	assert.NotContains(t, betaLine, ">")
	assert.Contains(t, betaLine, "disabled")
}

func TestSkillMgrViewEdit(t *testing.T) {
	m, _ := newSkillMgrTestModel(t, map[string]string{"alpha": "Body\n"})
	m.mode = skillModeEdit

	// Edit mode without a current skill falls back to the empty message.
	view := m.View()
	assert.Contains(t, view, "No installed skills found.")
	assert.Contains(t, view, "Enter toggle/save · Tab/↑/↓ navigate · Esc back")

	m.refreshInstalled()
	m.cursor = skillMgrIndex(t, m, "alpha")
	view = m.View()
	assert.Contains(t, view, "Edit Skill: alpha")
	assert.Contains(t, view, "> Enabled")
	assert.Contains(t, view, "disabled")
	assert.Contains(t, view, "  Save")

	m.cfg.Skills = map[string]config.SkillConfig{"alpha": {Enabled: true}}
	m.editCursor = 1
	view = m.View()
	assert.Contains(t, view, "  Enabled")
	assert.Contains(t, view, "enabled")
	assert.Contains(t, view, "> Save")
}

func TestSkillBoolLabel(t *testing.T) {
	assert.Contains(t, skillBoolLabel(true), "enabled")
	assert.NotContains(t, skillBoolLabel(true), "disabled")
	assert.Contains(t, skillBoolLabel(false), "disabled")
}

func TestSkillMgrProgramOptionsDefault(t *testing.T) {
	assert.Len(t, skillMgrProgramOptions(), 1)
}

func useHeadlessSkillMgr(t *testing.T, input io.Reader) {
	t.Helper()
	prev := skillMgrProgramOptions
	skillMgrProgramOptions = func() []tea.ProgramOption {
		return []tea.ProgramOption{
			tea.WithInput(input),
			tea.WithOutput(&bytes.Buffer{}),
			tea.WithoutRenderer(),
			tea.WithoutSignalHandler(),
		}
	}
	t.Cleanup(func() { skillMgrProgramOptions = prev })
}

func TestRunSkillMgr(t *testing.T) {
	m, cfgPath := newSkillMgrTestModel(t, nil)
	useHeadlessSkillMgr(t, iotest.OneByteReader(strings.NewReader("jkq")))
	require.NoError(t, runSkillMgr(m.cfg, cfgPath))
	assert.NoFileExists(t, cfgPath)
}

func TestRunSkillMgrListsAndTogglesInstalledSkill(t *testing.T) {
	m, cfgPath := newSkillMgrTestModel(t, nil)
	installed, err := skills.ListInstalled(m.cfg)
	require.NoError(t, err)
	require.NotEmpty(t, installed)
	first := installed[0]
	useHeadlessSkillMgr(t, iotest.OneByteReader(strings.NewReader("\r\r\x03")))
	require.NoError(t, runSkillMgr(m.cfg, cfgPath))

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, !first.Enabled, loaded.Skills[first.Name].Enabled)
}

func TestRunSkillMgrProgramError(t *testing.T) {
	m, cfgPath := newSkillMgrTestModel(t, nil)
	useHeadlessSkillMgr(t, &errReader{})
	assert.Error(t, runSkillMgr(m.cfg, cfgPath))
}
