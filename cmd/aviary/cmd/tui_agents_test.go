package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/support/templates"
)

// agentKey converts a key name into the tea.KeyMsg bubbletea would deliver.
func agentKey(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+u":
		return tea.KeyMsg{Type: tea.KeyCtrlU}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// pressAgent sends keys in order and returns the resulting model and the
// command produced by the last key.
func pressAgent(t *testing.T, m agentMgrModel, keys ...string) (agentMgrModel, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(agentKey(k))
		m = next.(agentMgrModel)
	}
	return m, cmd
}

// typeAgentText replaces the text input contents with value and submits it.
func typeAgentText(t *testing.T, m agentMgrModel, value string) agentMgrModel {
	t.Helper()
	require.Equal(t, agentModeTextEdit, m.mode)
	m, _ = pressAgent(t, m, "ctrl+u")
	if value != "" {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)})
		m = next.(agentMgrModel)
	}
	m, _ = pressAgent(t, m, "enter")
	return m
}

func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func agentMgrTestPaths(t *testing.T) (cfgPath, dataDir string) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, "xdg"))
	dataDir = filepath.Join(tmp, "data")
	store.SetDataDir(dataDir)
	t.Cleanup(func() { store.SetDataDir("") })
	return filepath.Join(tmp, "aviary.yaml"), dataDir
}

func TestAgentMgrNewModelDefaults(t *testing.T) {
	cfg := &config.Config{}
	m := newAgentMgrModel(cfg, "/tmp/x.yaml")
	assert.Nil(t, m.Init())
	assert.Equal(t, agentModeList, m.mode)
	assert.Equal(t, -1, m.editIdx)
	assert.Equal(t, -1, m.channelIdx)
	assert.Equal(t, -1, m.taskIdx)
	assert.Same(t, cfg, m.cfg)

	// Messages that are neither keys nor resizes leave the model untouched.
	next, cmd := m.Update(savedMsg{})
	assert.Nil(t, cmd)
	assert.Equal(t, m.mode, next.(agentMgrModel).mode)

	next, cmd = m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	assert.Nil(t, cmd)
	sized := next.(agentMgrModel)
	assert.Equal(t, 40, sized.width)
	assert.Equal(t, clampInputWidth(40), sized.textInput.Width)
	assert.Contains(t, sized.View(), strings.Repeat("─", 38))
	assert.NotContains(t, sized.View(), strings.Repeat("─", 39))
}

func TestAgentMgrQuitKeys(t *testing.T) {
	cases := []struct {
		name  string
		mode  agentMgrMode
		key   string
		quits bool
	}{
		{"list ctrl+c", agentModeList, "ctrl+c", true},
		{"list q", agentModeList, "q", true},
		{"list esc", agentModeList, "esc", true},
		{"edit menu ctrl+c", agentModeEditMenu, "ctrl+c", true},
		{"edit menu q does not quit", agentModeEditMenu, "q", false},
		{"text edit ctrl+c", agentModeTextEdit, "ctrl+c", true},
		{"channels ctrl+c", agentModeChannels, "ctrl+c", true},
		{"channel edit ctrl+c", agentModeChannelEdit, "ctrl+c", true},
		{"tasks ctrl+c", agentModeTasks, "ctrl+c", true},
		{"task edit ctrl+c", agentModeTaskEdit, "ctrl+c", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newAgentMgrModel(&config.Config{}, "")
			m.mode = tc.mode
			_, cmd := pressAgent(t, m, tc.key)
			assert.Equal(t, tc.quits, isQuitCmd(cmd))
		})
	}
}

func TestAgentMgrEscNavigatesBack(t *testing.T) {
	cases := []struct {
		name string
		mode agentMgrMode
		want agentMgrMode
	}{
		{"edit menu to list", agentModeEditMenu, agentModeList},
		{"channels to edit menu", agentModeChannels, agentModeEditMenu},
		{"channel edit to channels", agentModeChannelEdit, agentModeChannels},
		{"tasks to edit menu", agentModeTasks, agentModeEditMenu},
		{"task edit to tasks", agentModeTaskEdit, agentModeTasks},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newAgentMgrModel(&config.Config{}, "")
			m.mode = tc.mode
			m, cmd := pressAgent(t, m, "esc")
			assert.Nil(t, cmd)
			assert.Equal(t, tc.want, m.mode)
		})
	}
}

func TestAgentMgrListNavigationAndView(t *testing.T) {
	m := newAgentMgrModel(&config.Config{}, "")
	assert.Contains(t, m.View(), "No agents configured.")
	assert.Contains(t, m.View(), "Agent Manager")

	// Enter and delete on an empty list do nothing.
	m, cmd := pressAgent(t, m, "enter", "d")
	assert.Nil(t, cmd)
	assert.Equal(t, agentModeList, m.mode)

	m.cfg.Agents = []config.AgentConfig{
		{Name: "alpha", Model: "p/a"},
		{Name: "beta", Model: "p/b"},
	}
	m, _ = pressAgent(t, m, "up")
	assert.Equal(t, 0, m.cursor)
	m, _ = pressAgent(t, m, "down", "j")
	assert.Equal(t, 1, m.cursor, "cursor clamps at last agent")
	m, _ = pressAgent(t, m, "k")
	assert.Equal(t, 0, m.cursor)

	view := m.View()
	assert.Contains(t, view, "alpha")
	assert.Contains(t, view, "p/b")
	assert.Contains(t, view, "Enter edit · n new · d delete")

	m, _ = pressAgent(t, m, "down", " ")
	assert.Equal(t, agentModeEditMenu, m.mode)
	assert.False(t, m.isNew)
	assert.Equal(t, 1, m.editIdx)
	assert.Equal(t, "beta", m.draft.Name)
	assert.Contains(t, m.View(), "Editing beta")
}

func TestAgentMgrCreateAgentSavesConfigAndSyncsTemplate(t *testing.T) {
	cfgPath, _ := agentMgrTestPaths(t)
	m := newAgentMgrModel(&config.Config{}, cfgPath)

	m, _ = pressAgent(t, m, "n")
	require.Equal(t, agentModeEditMenu, m.mode)
	assert.True(t, m.isNew)
	assert.Contains(t, m.View(), "Editing <new agent>")

	fields := []struct {
		cursor int
		value  string
	}{
		{0, "helper"},
		{1, "anthropic/claude"},
		{2, "a/one, , b/two"},
		{3, "shared"},
		{4, "1200"},
		{5, "7"},
		{6, "Be nice."},
	}
	for _, f := range fields {
		m.editCursor = f.cursor
		var cmd tea.Cmd
		m, cmd = pressAgent(t, m, "enter")
		assert.NotNil(t, cmd, "opening the editor starts the cursor blink")
		assert.Contains(t, m.View(), "Enter save · Esc back")
		m = typeAgentText(t, m, f.value)
		assert.Equal(t, agentModeEditMenu, m.mode)
	}

	assert.Equal(t, config.AgentConfig{
		Name:         "helper",
		Model:        "anthropic/claude",
		Fallbacks:    []string{"a/one", "b/two"},
		Memory:       "shared",
		MemoryTokens: 1200,
		CompactKeep:  7,
		Rules:        "Be nice.",
	}, m.draft)
	view := m.View()
	assert.Contains(t, view, "a/one, b/two")
	assert.Contains(t, view, "1200")

	m.editCursor = 16
	m, cmd := pressAgent(t, m, "enter")
	require.NotNil(t, cmd)
	assert.Equal(t, agentModeList, m.mode)
	assert.Equal(t, 0, m.cursor)
	assert.Equal(t, "helper", m.pendingInit)
	assert.Equal(t, savedMsg{}, cmd())

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.Len(t, loaded.Agents, 1)
	assert.Equal(t, "helper", loaded.Agents[0].Name)
	assert.Equal(t, "anthropic/claude", loaded.Agents[0].Model)
	assert.Equal(t, []string{"a/one", "b/two"}, loaded.Agents[0].Fallbacks)
	assert.Equal(t, 1200, loaded.Agents[0].MemoryTokens)

	want, err := fs.ReadFile(templates.Agent(), "RULES.md")
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(store.AgentDir("helper"), "RULES.md"))
	require.NoError(t, err)
	assert.Equal(t, strings.ReplaceAll(string(want), "\r\n", "\n"), string(got))
}

func TestAgentMgrEditExistingAgentSaves(t *testing.T) {
	cfgPath, _ := agentMgrTestPaths(t)
	cfg := &config.Config{Agents: []config.AgentConfig{
		{Name: "alpha", Model: "p/a"},
		{Name: "beta", Model: "p/b"},
	}}
	m := newAgentMgrModel(cfg, cfgPath)
	m, _ = pressAgent(t, m, "down", "enter")

	// The editor opens pre-filled with the current model.
	m.editCursor = 1
	m, _ = pressAgent(t, m, "enter")
	assert.Equal(t, "p/b", m.textInput.Value())
	m = typeAgentText(t, m, "p/new")

	m.editCursor = 16
	m, cmd := pressAgent(t, m, "enter")
	require.NotNil(t, cmd)
	assert.Equal(t, "", m.err)
	assert.Equal(t, 1, m.cursor)
	assert.Equal(t, "", m.pendingInit)
	assert.Equal(t, "p/new", m.cfg.Agents[1].Model)
	assert.Equal(t, savedMsg{}, cmd())

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	require.Len(t, loaded.Agents, 2)
	assert.Equal(t, "p/new", loaded.Agents[1].Model)
	_, statErr := os.Stat(store.AgentDir("beta"))
	assert.True(t, os.IsNotExist(statErr), "editing an existing agent does not scaffold its directory")
}

func TestAgentMgrSaveValidation(t *testing.T) {
	existing := []config.AgentConfig{{Name: "alpha", Model: "p/a"}, {Name: "beta", Model: "p/b"}}
	cases := []struct {
		name    string
		isNew   bool
		editIdx int
		draft   config.AgentConfig
		wantErr string
	}{
		{"empty name", true, 2, config.AgentConfig{Name: "  ", Model: "p/x"}, "Agent name cannot be empty"},
		{"empty model", true, 2, config.AgentConfig{Name: "gamma"}, "Agent model cannot be empty"},
		{"duplicate new agent", true, 2, config.AgentConfig{Name: "alpha", Model: "p/x"}, `Agent "alpha" already exists`},
		{"rename onto another agent", false, 1, config.AgentConfig{Name: "alpha", Model: "p/x"}, `Agent "alpha" already exists`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Agents: append([]config.AgentConfig(nil), existing...)}
			m := newAgentMgrModel(cfg, filepath.Join(t.TempDir(), "aviary.yaml"))
			m.mode = agentModeEditMenu
			m.isNew = tc.isNew
			m.editIdx = tc.editIdx
			m.draft = tc.draft
			m.editCursor = 16
			m, cmd := pressAgent(t, m, "enter")
			assert.Nil(t, cmd)
			assert.Equal(t, agentModeEditMenu, m.mode)
			assert.Equal(t, tc.wantErr, m.err)
			assert.Contains(t, m.View(), tc.wantErr)
			assert.Equal(t, existing, m.cfg.Agents)
		})
	}
}

func TestAgentMgrDeleteAgent(t *testing.T) {
	cases := []struct {
		name       string
		cursor     int
		key        string
		wantNames  []string
		wantCursor int
	}{
		{"delete first keeps cursor", 0, "d", []string{"b", "c"}, 0},
		{"delete middle keeps cursor", 1, "delete", []string{"a", "c"}, 1},
		{"delete last moves cursor up", 2, "d", []string{"a", "b"}, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgPath, _ := agentMgrTestPaths(t)
			cfg := &config.Config{Agents: []config.AgentConfig{
				{Name: "a", Model: "p/a"}, {Name: "b", Model: "p/b"}, {Name: "c", Model: "p/c"},
			}}
			m := newAgentMgrModel(cfg, cfgPath)
			m.cursor = tc.cursor
			m, cmd := pressAgent(t, m, tc.key)
			require.NotNil(t, cmd)
			assert.Equal(t, tc.wantCursor, m.cursor)
			assert.Equal(t, savedMsg{}, cmd())

			loaded, err := config.Load(cfgPath)
			require.NoError(t, err)
			var names []string
			for _, a := range loaded.Agents {
				names = append(names, a.Name)
			}
			assert.Equal(t, tc.wantNames, names)
		})
	}
}

func TestAgentMgrSaveConfigErrors(t *testing.T) {
	tmp := t.TempDir()
	blocker := filepath.Join(tmp, "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))

	t.Run("config path under a file", func(t *testing.T) {
		m := newAgentMgrModel(&config.Config{Agents: []config.AgentConfig{{Name: "a", Model: "p/a"}}},
			filepath.Join(blocker, "aviary.yaml"))
		_, cmd := pressAgent(t, m, "d")
		require.NotNil(t, cmd)
		msg, ok := cmd().(errMsg)
		require.True(t, ok)
		assert.Contains(t, string(msg), "creating config dir")
	})

	t.Run("template sync failure", func(t *testing.T) {
		store.SetDataDir(blocker)
		t.Cleanup(func() { store.SetDataDir("") })
		m := newAgentMgrModel(&config.Config{}, filepath.Join(tmp, "aviary.yaml"))
		m.mode = agentModeEditMenu
		m.isNew = true
		m.draft = config.AgentConfig{Name: "n", Model: "p/n"}
		m.editCursor = 16
		_, cmd := pressAgent(t, m, "enter")
		require.NotNil(t, cmd)
		_, ok := cmd().(errMsg)
		assert.True(t, ok)
		loaded, err := config.Load(filepath.Join(tmp, "aviary.yaml"))
		require.NoError(t, err)
		require.Len(t, loaded.Agents, 1, "config is written before the template sync fails")
	})
}

func TestAgentMgrEditMenuNavigation(t *testing.T) {
	m := newAgentMgrModel(&config.Config{}, "")
	m, _ = pressAgent(t, m, "n", "up", "shift+tab")
	assert.Equal(t, 0, m.editCursor)
	for range 30 {
		m, _ = pressAgent(t, m, "tab")
	}
	assert.Equal(t, 17, m.editCursor, "cursor clamps at the Back item")
	m, _ = pressAgent(t, m, "k", "shift+tab")
	assert.Equal(t, 15, m.editCursor)
	m, _ = pressAgent(t, m, "j", "down")
	assert.Equal(t, 17, m.editCursor)

	// Back discards the draft and returns to the list.
	m.draft.Name = "discard-me"
	m, _ = pressAgent(t, m, " ")
	assert.Equal(t, agentModeList, m.mode)
	assert.Empty(t, m.cfg.Agents)
}

func TestAgentMgrTextEditEscDiscardsValue(t *testing.T) {
	cases := []struct {
		name   string
		target agentTextTarget
		parent agentMgrMode
	}{
		{"agent field", agentFieldName, agentModeEditMenu},
		{"channel field", chFieldURL, agentModeChannelEdit},
		{"task field", taskFieldTarget, agentModeTaskEdit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newAgentMgrModel(&config.Config{}, "")
			next, _ := m.openTextEditor(tc.target, "orig", "")
			m = next.(agentMgrModel)
			next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-typed")})
			m = next.(agentMgrModel)
			assert.Equal(t, "orig-typed", m.textInput.Value())
			m, _ = pressAgent(t, m, "esc")
			assert.Equal(t, tc.parent, m.mode)
			assert.False(t, m.textInput.Focused())
			assert.Equal(t, config.AgentConfig{}, m.draft)
			assert.Equal(t, config.ChannelConfig{}, m.channel)
			assert.Equal(t, config.TaskConfig{}, m.task)
		})
	}
}

func TestAgentMgrPermissionFields(t *testing.T) {
	cases := []struct {
		name        string
		start       *config.PermissionsConfig
		cursor      int
		wantInitial string
		input       string
		want        *config.PermissionsConfig
		wantView    string
	}{
		{"preset full", nil, 7, "", "full", &config.PermissionsConfig{Preset: config.PermissionsPresetFull}, "full"},
		{"preset shows current", &config.PermissionsConfig{Preset: config.PermissionsPresetMinimal}, 7, "minimal", "standard", nil, "standard"},
		{"preset blank clears", &config.PermissionsConfig{Preset: config.PermissionsPresetFull, Tools: []string{"t"}}, 7, "full", "", &config.PermissionsConfig{Tools: []string{"t"}}, "standard"},
		{"preset unknown ignored", &config.PermissionsConfig{Preset: config.PermissionsPresetMinimal}, 7, "minimal", "bogus", &config.PermissionsConfig{Preset: config.PermissionsPresetMinimal}, "minimal"},
		{"allowed tools", nil, 8, "", "read, write", &config.PermissionsConfig{Tools: []string{"read", "write"}}, "read, write"},
		{"allowed tools cleared", &config.PermissionsConfig{Tools: []string{"x"}}, 8, "x", "", nil, ""},
		{"disabled tools", &config.PermissionsConfig{DisabledTools: []string{"old"}}, 9, "old", "exec", &config.PermissionsConfig{DisabledTools: []string{"exec"}}, "exec"},
		{"filesystem paths", nil, 10, "", "/a, !/b", &config.PermissionsConfig{Filesystem: &config.FilesystemPermissionsConfig{AllowedPaths: []string{"/a", "!/b"}}}, "/a, !/b"},
		{"filesystem paths cleared", &config.PermissionsConfig{Filesystem: &config.FilesystemPermissionsConfig{AllowedPaths: []string{"/a"}}}, 10, "/a", "", nil, ""},
		{"exec commands", nil, 11, "", "ls, !rm", &config.PermissionsConfig{Exec: &config.ExecPermissionsConfig{AllowedCommands: []string{"ls", "!rm"}}}, "ls, !rm"},
		{"exec commands cleared keeps shell", &config.PermissionsConfig{Exec: &config.ExecPermissionsConfig{AllowedCommands: []string{"ls"}, Shell: "bash"}}, 11, "ls", "", &config.PermissionsConfig{Exec: &config.ExecPermissionsConfig{Shell: "bash"}}, "bash"},
		{"exec commands cleared without exec", &config.PermissionsConfig{Tools: []string{"t"}}, 11, "", "", &config.PermissionsConfig{Tools: []string{"t"}}, "t"},
		{"exec shell", &config.PermissionsConfig{Exec: &config.ExecPermissionsConfig{AllowedCommands: []string{"ls"}}}, 13, "", "pwsh", &config.PermissionsConfig{Exec: &config.ExecPermissionsConfig{AllowedCommands: []string{"ls"}, Shell: "pwsh"}}, "pwsh"},
		{"exec shell on empty permissions", nil, 13, "", "zsh", &config.PermissionsConfig{Exec: &config.ExecPermissionsConfig{Shell: "zsh"}}, "zsh"},
		{"exec shell cleared", &config.PermissionsConfig{Exec: &config.ExecPermissionsConfig{Shell: "zsh"}}, 13, "zsh", "", nil, ""},
		{"exec shell cleared without exec", nil, 13, "", " ", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newAgentMgrModel(&config.Config{}, "")
			m.mode = agentModeEditMenu
			m.draft = config.AgentConfig{Name: "a", Permissions: tc.start}
			m.editCursor = tc.cursor
			m, _ = pressAgent(t, m, "enter")
			require.Equal(t, agentModeTextEdit, m.mode)
			assert.Equal(t, tc.wantInitial, m.textInput.Value())
			m = typeAgentText(t, m, tc.input)
			assert.Equal(t, agentModeEditMenu, m.mode)
			assert.Equal(t, tc.want, m.draft.Permissions)
			if tc.wantView != "" {
				assert.Contains(t, m.View(), tc.wantView)
			}
		})
	}
}

func TestAgentMgrExecInterpolateToggle(t *testing.T) {
	m := newAgentMgrModel(&config.Config{}, "")
	m.mode = agentModeEditMenu
	m.editCursor = 12
	assert.Equal(t, [2]string{"Exec interpolate", "off"}, m.editMenuItems()[12])

	m, cmd := pressAgent(t, m, "enter")
	assert.Nil(t, cmd)
	assert.Equal(t, agentModeEditMenu, m.mode)
	require.NotNil(t, m.draft.Permissions)
	assert.Equal(t, &config.ExecPermissionsConfig{ShellInterpolate: true}, m.draft.Permissions.Exec)
	assert.Equal(t, [2]string{"Exec interpolate", "on"}, m.editMenuItems()[12])

	m, _ = pressAgent(t, m, "enter")
	assert.Nil(t, m.draft.Permissions, "turning interpolation back off compacts empty permissions away")
}

func TestAgentMgrChannelEditing(t *testing.T) {
	m := newAgentMgrModel(&config.Config{}, "")
	m.mode = agentModeEditMenu
	m.draft = config.AgentConfig{Name: "a", Model: "p/a"}
	m.editCursor = 14
	m, _ = pressAgent(t, m, "enter")
	require.Equal(t, agentModeChannels, m.mode)
	assert.Equal(t, 0, m.channelIdx)
	assert.Contains(t, m.View(), "No channels configured.")

	// Delete and enter do nothing without channels.
	m, _ = pressAgent(t, m, "d", "enter")
	assert.Equal(t, agentModeChannels, m.mode)

	m, _ = pressAgent(t, m, "n")
	require.Equal(t, agentModeChannelEdit, m.mode)
	assert.Equal(t, "signal", m.channel.Type)
	assert.Equal(t, "*", joinAllowFrom(m.channel.AllowFrom))
	view := m.View()
	assert.Contains(t, view, "Channel")
	assert.Contains(t, view, "Show typing")

	texts := []struct {
		cursor  int
		initial string
		input   string
	}{
		{0, "signal", "slack"},
		{1, "", "auth:slack"},
		{2, "", "bot-1"},
		{3, "", "127.0.0.1:1"},
		{4, "*", "U1, U2"},
		{5, "", "p/m"},
		{6, "", "f/1, f/2"},
	}
	for _, tx := range texts {
		m.fieldCursor = tx.cursor
		m, _ = pressAgent(t, m, "enter")
		require.Equal(t, agentModeTextEdit, m.mode)
		assert.Equal(t, tx.initial, m.textInput.Value())
		m = typeAgentText(t, m, tx.input)
		assert.Equal(t, agentModeChannelEdit, m.mode)
	}
	for cursor := 7; cursor <= 10; cursor++ {
		m.fieldCursor = cursor
		m, _ = pressAgent(t, m, " ")
	}
	off := false
	assert.Equal(t, config.ChannelConfig{
		Type:             "slack",
		Token:            "auth:slack",
		ID:               "bot-1",
		URL:              "127.0.0.1:1",
		AllowFrom:        []config.AllowFromEntry{{From: "U1"}, {From: "U2"}},
		Model:            "p/m",
		Fallbacks:        []string{"f/1", "f/2"},
		ShowTyping:       &off,
		ReactToEmoji:     &off,
		ReplyToReplies:   &off,
		SendReadReceipts: &off,
	}, m.channel)
	assert.Contains(t, m.View(), "U1, U2")

	// Save appends the new channel.
	m.fieldCursor = 11
	m, _ = pressAgent(t, m, "enter")
	assert.Equal(t, agentModeChannels, m.mode)
	require.Len(t, m.draft.Channels, 1)
	assert.Equal(t, 0, m.channelIdx)
	assert.Contains(t, m.View(), "bot-1")
	assert.Equal(t, "1 configured", m.editMenuItems()[14][1])

	// Add a second channel with no ID; its URL is shown instead.
	m, _ = pressAgent(t, m, "n")
	assert.Equal(t, 1, m.channelIdx)
	m.fieldCursor = 3
	m, _ = pressAgent(t, m, "enter")
	m = typeAgentText(t, m, "10.0.0.1:2")
	m.fieldCursor = 11
	m, _ = pressAgent(t, m, "enter")
	require.Len(t, m.draft.Channels, 2)
	assert.Equal(t, 1, m.channelIdx)
	assert.Contains(t, m.View(), "10.0.0.1:2")

	// Editing an existing channel replaces it in place.
	m, _ = pressAgent(t, m, "up", "up")
	assert.Equal(t, 0, m.channelIdx)
	m, _ = pressAgent(t, m, "enter")
	require.Equal(t, agentModeChannelEdit, m.mode)
	assert.Equal(t, "bot-1", m.channel.ID)
	m.fieldCursor = 2
	m, _ = pressAgent(t, m, "enter")
	m = typeAgentText(t, m, "bot-2")
	m.fieldCursor = 11
	m, _ = pressAgent(t, m, "enter")
	require.Len(t, m.draft.Channels, 2)
	assert.Equal(t, "bot-2", m.draft.Channels[0].ID)

	// Back discards changes.
	m, _ = pressAgent(t, m, "enter")
	m.fieldCursor = 2
	m, _ = pressAgent(t, m, "enter")
	m = typeAgentText(t, m, "discarded")
	m.fieldCursor = 12
	m, _ = pressAgent(t, m, "enter")
	assert.Equal(t, agentModeChannels, m.mode)
	assert.Equal(t, "bot-2", m.draft.Channels[0].ID)

	// Navigation clamps, then deleting the last channel moves the cursor up.
	m, _ = pressAgent(t, m, "j", "down", "down")
	assert.Equal(t, 1, m.channelIdx)
	m, _ = pressAgent(t, m, "delete")
	require.Len(t, m.draft.Channels, 1)
	assert.Equal(t, 0, m.channelIdx)
	m, _ = pressAgent(t, m, "k", "d")
	assert.Empty(t, m.draft.Channels)
	assert.Equal(t, 0, m.channelIdx)
}

func TestAgentMgrChannelEditorNavigation(t *testing.T) {
	m := newAgentMgrModel(&config.Config{}, "")
	m.mode = agentModeChannelEdit
	m, _ = pressAgent(t, m, "up", "shift+tab")
	assert.Equal(t, 0, m.fieldCursor)
	for range 20 {
		m, _ = pressAgent(t, m, "tab")
	}
	assert.Equal(t, 12, m.fieldCursor)
	m, _ = pressAgent(t, m, "k")
	assert.Equal(t, 11, m.fieldCursor)
	m, _ = pressAgent(t, m, "j", "down")
	assert.Equal(t, 12, m.fieldCursor)
}

func TestAgentMgrTaskEditing(t *testing.T) {
	m := newAgentMgrModel(&config.Config{}, "")
	m.mode = agentModeEditMenu
	m.draft = config.AgentConfig{Name: "a", Model: "p/a"}
	m.editCursor = 15
	m, _ = pressAgent(t, m, "enter")
	require.Equal(t, agentModeTasks, m.mode)
	assert.Equal(t, 0, m.taskIdx)
	assert.Contains(t, m.View(), "No tasks configured.")

	m, _ = pressAgent(t, m, "d", "enter")
	assert.Equal(t, agentModeTasks, m.mode)

	m, _ = pressAgent(t, m, "n")
	require.Equal(t, agentModeTaskEdit, m.mode)
	assert.Equal(t, "prompt", m.task.Type)
	assert.Contains(t, m.View(), "Prompt")

	texts := []struct {
		cursor int
		input  string
	}{
		{0, "daily"},
		{2, "0 9 * * *"},
		{3, "2026-01-01T00:00:00Z"},
		{5, "*.md"},
		{6, "summarize"},
		{7, "route:signal:+1:+2"},
	}
	for _, tx := range texts {
		m.fieldCursor = tx.cursor
		m, _ = pressAgent(t, m, "enter")
		require.Equal(t, agentModeTextEdit, m.mode)
		m = typeAgentText(t, m, tx.input)
		assert.Equal(t, agentModeTaskEdit, m.mode)
	}
	m.fieldCursor = 4
	m, _ = pressAgent(t, m, " ")
	assert.Equal(t, config.TaskConfig{
		Name:     "daily",
		Type:     "prompt",
		Schedule: "0 9 * * *",
		StartAt:  "2026-01-01T00:00:00Z",
		RunOnce:  true,
		Watch:    "*.md",
		Prompt:   "summarize",
		Target:   "route:signal:+1:+2",
	}, m.task)
	view := m.View()
	assert.Contains(t, view, "summarize")
	assert.Contains(t, view, "on")

	m.fieldCursor = 8
	m, _ = pressAgent(t, m, "enter")
	assert.Equal(t, agentModeTasks, m.mode)
	require.Len(t, m.draft.Tasks, 1)
	assert.Equal(t, 0, m.taskIdx)
	assert.Contains(t, m.View(), "0 9 * * *", "task list summarizes by schedule first")
	assert.Equal(t, "1 configured", m.editMenuItems()[15][1])

	// A second task switched to script type uses the Script editor.
	m, _ = pressAgent(t, m, "n")
	assert.Equal(t, 1, m.taskIdx)
	m.fieldCursor = 1
	m, _ = pressAgent(t, m, "enter")
	assert.Equal(t, "prompt", m.textInput.Value())
	m = typeAgentText(t, m, "script")
	assert.Equal(t, "script", m.task.Type)
	assert.Equal(t, "Script", m.taskFieldLabels()[6])
	m.fieldCursor = 6
	m, _ = pressAgent(t, m, "enter")
	assert.Equal(t, taskFieldScript, m.textTarget)
	m = typeAgentText(t, m, "print(1)")
	assert.Equal(t, "print(1)", m.task.Prompt)
	m.fieldCursor = 8
	m, _ = pressAgent(t, m, "enter")
	require.Len(t, m.draft.Tasks, 2)
	assert.Contains(t, m.View(), "print(1)", "script content is the summary without schedule or watch")

	// Any type other than "script" falls back to prompt.
	m, _ = pressAgent(t, m, "enter")
	assert.Equal(t, "script", m.task.Type)
	m.fieldCursor = 1
	m, _ = pressAgent(t, m, "enter")
	m = typeAgentText(t, m, "other")
	assert.Equal(t, "prompt", m.task.Type)
	m.fieldCursor = 8
	m, _ = pressAgent(t, m, "enter")
	assert.Equal(t, "prompt", m.draft.Tasks[1].Type)

	// Back discards edits to an existing task.
	m, _ = pressAgent(t, m, "up", "up", "enter")
	assert.Equal(t, "daily", m.task.Name)
	m.fieldCursor = 0
	m, _ = pressAgent(t, m, "enter")
	m = typeAgentText(t, m, "renamed")
	m.fieldCursor = 9
	m, _ = pressAgent(t, m, "enter")
	assert.Equal(t, agentModeTasks, m.mode)
	assert.Equal(t, "daily", m.draft.Tasks[0].Name)

	// Navigation clamps, then deleting the last task moves the cursor up.
	m, _ = pressAgent(t, m, "j", "down", "down")
	assert.Equal(t, 1, m.taskIdx)
	m, _ = pressAgent(t, m, "delete")
	require.Len(t, m.draft.Tasks, 1)
	assert.Equal(t, 0, m.taskIdx)
	m, _ = pressAgent(t, m, "k", "d")
	assert.Empty(t, m.draft.Tasks)
}

func TestAgentMgrTaskEditorNavigation(t *testing.T) {
	m := newAgentMgrModel(&config.Config{}, "")
	m.mode = agentModeTaskEdit
	m, _ = pressAgent(t, m, "up", "shift+tab")
	assert.Equal(t, 0, m.fieldCursor)
	for range 20 {
		m, _ = pressAgent(t, m, "tab")
	}
	assert.Equal(t, 9, m.fieldCursor)
	m, _ = pressAgent(t, m, "k")
	assert.Equal(t, 8, m.fieldCursor)
	m, _ = pressAgent(t, m, "j", "down")
	assert.Equal(t, 9, m.fieldCursor)
}

func TestAgentMgrTaskListEmptySummary(t *testing.T) {
	m := newAgentMgrModel(&config.Config{}, "")
	m.mode = agentModeTasks
	m.draft.Tasks = []config.TaskConfig{{Name: "blank"}, {Name: "watcher", Watch: "*.go"}}
	view := m.View()
	assert.Contains(t, view, "(empty)")
	assert.Contains(t, view, "*.go")
}

func TestSplitCSV(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{" , ,", nil},
		{"a", []string{"a"}},
		{" a , ,b ", []string{"a", "b"}},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, splitCSV(tc.in), "input %q", tc.in)
	}
}
