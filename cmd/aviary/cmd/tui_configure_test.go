package cmd

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
)

// newConfigureTestStore returns an auth store backed by a temp file and points
// cfgFile at a temp config so provider summaries never read real user state.
func newConfigureTestStore(t *testing.T) (*authpkg.FileStore, string) {
	t.Helper()
	tmp := t.TempDir()
	st, err := authpkg.NewFileStore(filepath.Join(tmp, "auth.json"))
	require.NoError(t, err)
	cfgPath := filepath.Join(tmp, "aviary.yaml")
	prev := cfgFile
	cfgFile = cfgPath
	t.Cleanup(func() { cfgFile = prev })
	return st, cfgPath
}

func TestConfigureProvidersSummary(t *testing.T) {
	st, cfgPath := newConfigureTestStore(t)
	assert.Equal(t, "0/6 connected", configureProvidersSummary(st))

	require.NoError(t, st.Set("anthropic:oauth", "tok"))
	require.NoError(t, st.Set("openai:default", "key"))
	require.NoError(t, st.Set("unrelated:default", "key"))
	assert.Equal(t, "2/6 connected", configureProvidersSummary(st))

	cfg := config.Default()
	cfg.Models.Providers = map[string]config.ProviderConfig{
		"vllm":   {BaseURI: "http://127.0.0.1:8000"},
		"ollama": {BaseURI: "  "},
	}
	require.NoError(t, config.Save(cfgPath, &cfg))
	assert.Equal(t, "3/6 connected", configureProvidersSummary(st))

	cfg.Models.Providers["ollama"] = config.ProviderConfig{BaseURI: "http://127.0.0.1:11434"}
	require.NoError(t, config.Save(cfgPath, &cfg))
	assert.Equal(t, "4/6 connected", configureProvidersSummary(st))

	// More connected keys than the displayed total are clamped.
	for _, key := range []string{"anthropic:default", "openai:oauth", "gemini:oauth", "gemini:default", "meta:default"} {
		require.NoError(t, st.Set(key, "x"))
	}
	assert.Equal(t, "6/6 connected", configureProvidersSummary(st))
}

func TestConfigureProvidersSummaryFromItems(t *testing.T) {
	assert.Equal(t, "", configureProvidersSummaryFromItems(nil))
	items := []configureMenuItem{
		{selection: configureMenuGeneral, summary: "general"},
		{selection: configureMenuProviders, summary: "2/6 connected"},
	}
	assert.Equal(t, "2/6 connected", configureProvidersSummaryFromItems(items))
}

func TestConfigureAgentsSummary(t *testing.T) {
	cfg := config.Default()
	cfg.Agents = nil
	assert.Equal(t, "No agents configured", configureAgentsSummary(&cfg))
	cfg.Agents = []config.AgentConfig{{Name: "a"}}
	assert.Equal(t, "1 agent configured", configureAgentsSummary(&cfg))
	cfg.Agents = append(cfg.Agents, config.AgentConfig{Name: "b"}, config.AgentConfig{Name: "c"})
	assert.Equal(t, "3 agents configured", configureAgentsSummary(&cfg))
}

func TestConfigureSkillsSummaryCountsOnlyEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Skills = map[string]config.SkillConfig{
		"a": {Enabled: true},
		"b": {Enabled: true},
		"c": {Enabled: false},
	}
	assert.Equal(t, "2 skills enabled", configureSkillsSummary(&cfg))
}

func TestConfigureServerSummary(t *testing.T) {
	t.Setenv("AVIARY_CONFIG_SERVER_NO_TLS", "")
	tests := []struct {
		name   string
		server config.ServerConfig
		want   string
	}{
		{"local tls", config.ServerConfig{Port: 16677}, "https on localhost:16677"},
		{"external", config.ServerConfig{Port: 8443, ExternalAccess: true}, "https on external:8443"},
		{"plain http", config.ServerConfig{Port: 8080, NoTLS: true}, "http on localhost:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Server = tt.server
			assert.Equal(t, tt.want, configureServerSummary(&cfg))
		})
	}

	t.Run("env override", func(t *testing.T) {
		t.Setenv("AVIARY_CONFIG_SERVER_NO_TLS", "true")
		cfg := config.Default()
		cfg.Server = config.ServerConfig{Port: 9000}
		assert.Equal(t, "http on localhost:9000", configureServerSummary(&cfg))
	})
}

func TestConfigureBrowserSummary(t *testing.T) {
	no := false
	tests := []struct {
		name    string
		browser config.BrowserConfig
		want    string
	}{
		{"defaults", config.BrowserConfig{}, "headed, reuse tabs"},
		{"headless", config.BrowserConfig{Headless: true}, "headless, reuse tabs"},
		{"new tabs", config.BrowserConfig{ReuseTabs: &no}, "headed, new tabs only"},
		{"custom binary", config.BrowserConfig{Headless: true, Binary: "/opt/chrome"}, "headless, reuse tabs, custom binary"},
		{"blank binary", config.BrowserConfig{Binary: "   "}, "headed, reuse tabs"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Browser = tt.browser
			assert.Equal(t, tt.want, configureBrowserSummary(&cfg))
		})
	}
}

func TestConfigureSchedulerSummary(t *testing.T) {
	tests := []struct {
		name        string
		concurrency any
		want        string
	}{
		{"unset", nil, "Concurrency auto"},
		{"auto", "auto", "Concurrency auto"},
		{"number", 4, "Concurrency 4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Scheduler.Concurrency = tt.concurrency
			assert.Equal(t, tt.want, configureSchedulerSummary(&cfg))
		})
	}
}

func TestConfigureGeneralSummary(t *testing.T) {
	t.Setenv("AVIARY_CONFIG_SERVER_NO_TLS", "")
	cfg := config.Default()
	cfg.Server = config.ServerConfig{Port: 16677}
	cfg.Browser = config.BrowserConfig{}
	cfg.Scheduler.Concurrency = 2
	cfg.Search.Web.BraveAPIKey = ""
	assert.Equal(t, "https on localhost:16677 · headed, reuse tabs · Concurrency 2 · Browser fallback only", configureGeneralSummary(&cfg))

	cfg.Search.Web.BraveAPIKey = " auth:brave:default "
	assert.Equal(t, "https on localhost:16677 · headed, reuse tabs · Concurrency 2 · Brave auth:brave:default", configureGeneralSummary(&cfg))
}

func TestConfigureIntHelpers(t *testing.T) {
	assert.Equal(t, 1, minInt(1, 2))
	assert.Equal(t, 1, minInt(2, 1))
	assert.Equal(t, 2, maxInt(1, 2))
	assert.Equal(t, 2, maxInt(2, 1))
	assert.Equal(t, 80, widthOrDefault(0))
	assert.Equal(t, 80, widthOrDefault(-5))
	assert.Equal(t, 120, widthOrDefault(120))
}

func newTestConfigureMenu(t *testing.T) configureMenuModel {
	t.Helper()
	st, _ := newConfigureTestStore(t)
	cfg := config.Default()
	cfg.Agents = []config.AgentConfig{{Name: "a"}}
	return newConfigureMenuModel(&cfg, st)
}

func configureMenuKey(t *testing.T, m configureMenuModel, key tea.KeyMsg) (configureMenuModel, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(key)
	fm, ok := next.(configureMenuModel)
	require.True(t, ok)
	return fm, cmd
}

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestNewConfigureMenuModel(t *testing.T) {
	m := newTestConfigureMenu(t)
	assert.Nil(t, m.Init())
	assert.Equal(t, 0, m.cursor)
	assert.Equal(t, configureMenuQuit, m.selection)

	want := []configureMenuSelection{
		configureMenuGeneral, configureMenuProviders, configureMenuAgents, configureMenuSkills,
		configureMenuServer, configureMenuBrowser, configureMenuScheduler, configureMenuQuit,
	}
	require.Len(t, m.items, len(want))
	for i, sel := range want {
		assert.Equal(t, sel, m.items[i].selection)
		assert.NotEmpty(t, m.items[i].title)
		assert.NotEmpty(t, m.items[i].detail)
	}
	assert.Equal(t, "0/6 connected", m.items[1].summary)
	assert.Equal(t, "1 agent configured", m.items[2].summary)
	assert.Equal(t, "Exit configure", m.items[7].summary)
}

func TestConfigureMenuNavigation(t *testing.T) {
	m := newTestConfigureMenu(t)

	// Moving up at the top stays put.
	for _, key := range []tea.KeyMsg{{Type: tea.KeyUp}, runeKey('k'), {Type: tea.KeyShiftTab}} {
		var cmd tea.Cmd
		m, cmd = configureMenuKey(t, m, key)
		assert.Nil(t, cmd)
		assert.Equal(t, 0, m.cursor)
	}

	m, _ = configureMenuKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, m.cursor)
	m, _ = configureMenuKey(t, m, runeKey('j'))
	assert.Equal(t, 2, m.cursor)
	m, _ = configureMenuKey(t, m, tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, 3, m.cursor)
	m, _ = configureMenuKey(t, m, runeKey('k'))
	assert.Equal(t, 2, m.cursor)

	// Moving down at the bottom stays put.
	for range len(m.items) + 2 {
		m, _ = configureMenuKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	assert.Equal(t, len(m.items)-1, m.cursor)

	// Unhandled keys and messages change nothing.
	before := m
	m, cmd := configureMenuKey(t, m, runeKey('x'))
	assert.Nil(t, cmd)
	assert.Equal(t, before, m)
	next, cmd := m.Update("not a key")
	assert.Nil(t, cmd)
	assert.Equal(t, before, next)
}

func TestConfigureMenuSelect(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeySpace, Runes: []rune{' '}}} {
		t.Run(key.String(), func(t *testing.T) {
			m := newTestConfigureMenu(t)
			m, _ = configureMenuKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
			m, _ = configureMenuKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
			m, cmd := configureMenuKey(t, m, key)
			require.NotNil(t, cmd)
			assert.IsType(t, tea.QuitMsg{}, cmd())
			assert.Equal(t, configureMenuAgents, m.selection)
		})
	}
}

func TestConfigureMenuQuitKeys(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyCtrlC}, runeKey('q'), {Type: tea.KeyEsc}} {
		t.Run(key.String(), func(t *testing.T) {
			m := newTestConfigureMenu(t)
			m.selection = configureMenuAgents
			m, cmd := configureMenuKey(t, m, key)
			require.NotNil(t, cmd)
			assert.IsType(t, tea.QuitMsg{}, cmd())
			assert.Equal(t, configureMenuQuit, m.selection)
		})
	}
}

func TestConfigureMenuView(t *testing.T) {
	m := newTestConfigureMenu(t)

	view := m.View()
	assert.Contains(t, view, "Configure Aviary")
	assert.Contains(t, view, "8 available")
	assert.Contains(t, view, "0/6 connected")
	for _, item := range m.items {
		assert.Contains(t, view, item.title)
		assert.Contains(t, view, item.summary)
	}
	assert.Contains(t, view, m.items[0].detail)
	assert.Contains(t, view, "Enter select")
	// Default width of 80 gives a 72-character rule.
	assert.Contains(t, view, strings.Repeat("─", 72))
	assert.NotContains(t, view, strings.Repeat("─", 73))

	next, cmd := m.Update(tea.WindowSizeMsg{Width: 30, Height: 20})
	assert.Nil(t, cmd)
	m = next.(configureMenuModel)
	assert.Equal(t, 30, m.width)
	assert.Equal(t, 20, m.height)
	m, _ = configureMenuKey(t, m, tea.KeyMsg{Type: tea.KeyDown})
	view = m.View()
	assert.Contains(t, view, m.items[1].detail)
	assert.NotContains(t, view, m.items[0].detail)
	// Narrow terminals keep the 24-character minimum rule.
	assert.Contains(t, view, strings.Repeat("─", 24))
	assert.NotContains(t, view, strings.Repeat("─", 29))

	// An out-of-range cursor omits the detail pane.
	m.cursor = len(m.items)
	view = m.View()
	for _, item := range m.items {
		assert.NotContains(t, view, item.detail)
	}
}

// useHeadlessConfigureMenu runs the configure menu program on scripted input
// without a terminal.
func useHeadlessConfigureMenu(t *testing.T, input string) {
	t.Helper()
	prev := configureMenuProgramOptions
	configureMenuProgramOptions = func() []tea.ProgramOption {
		return []tea.ProgramOption{
			tea.WithInput(iotest.OneByteReader(strings.NewReader(input))),
			tea.WithOutput(&bytes.Buffer{}),
			tea.WithoutRenderer(),
			tea.WithoutSignalHandler(),
		}
	}
	t.Cleanup(func() { configureMenuProgramOptions = prev })
}

func TestConfigureMenuProgramOptionsDefault(t *testing.T) {
	assert.Len(t, configureMenuProgramOptions(), 1)
}

func TestRunConfigureMenu(t *testing.T) {
	st, _ := newConfigureTestStore(t)
	cfg := config.Default()

	useHeadlessConfigureMenu(t, "jj\r")
	got, err := runConfigureMenu(&cfg, st)
	require.NoError(t, err)
	assert.Equal(t, configureMenuAgents, got)

	useHeadlessConfigureMenu(t, "q")
	got, err = runConfigureMenu(&cfg, st)
	require.NoError(t, err)
	assert.Equal(t, configureMenuQuit, got)
}

func TestRunConfigureMenuProgramError(t *testing.T) {
	st, _ := newConfigureTestStore(t)
	cfg := config.Default()

	prev := configureMenuProgramOptions
	configureMenuProgramOptions = func() []tea.ProgramOption {
		return []tea.ProgramOption{
			tea.WithInput(&errReader{}),
			tea.WithOutput(&bytes.Buffer{}),
			tea.WithoutRenderer(),
			tea.WithoutSignalHandler(),
		}
	}
	t.Cleanup(func() { configureMenuProgramOptions = prev })

	got, err := runConfigureMenu(&cfg, st)
	assert.Error(t, err)
	assert.Equal(t, configureMenuQuit, got)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("input failed") }

// stubConfigureWizard replaces the wizard's menu with a scripted sequence of
// selections and every section editor with a recorder.
func stubConfigureWizard(t *testing.T, selections []configureMenuSelection, editorErr error) *[]string {
	t.Helper()
	var calls []string
	prevMenu := configureMenuRunner
	prevGeneral, prevProviders, prevAgents := configureGeneralForm, configureProviderMgr, configureAgentMgr
	prevSkills, prevServer, prevBrowser, prevScheduler := configureSkillMgr, configureServerForm, configureBrowserForm, configureSchedulerForm
	t.Cleanup(func() {
		configureMenuRunner = prevMenu
		configureGeneralForm, configureProviderMgr, configureAgentMgr = prevGeneral, prevProviders, prevAgents
		configureSkillMgr, configureServerForm, configureBrowserForm, configureSchedulerForm = prevSkills, prevServer, prevBrowser, prevScheduler
	})

	configureMenuRunner = func(*config.Config, authpkg.Store) (configureMenuSelection, error) {
		if len(selections) == 0 {
			t.Fatal("configure menu shown more times than scripted")
		}
		sel := selections[0]
		selections = selections[1:]
		return sel, nil
	}
	editor := func(name string) func(*config.Config, string) error {
		return func(_ *config.Config, cfgPath string) error {
			calls = append(calls, name+":"+cfgPath)
			return editorErr
		}
	}
	configureGeneralForm = editor("general")
	configureAgentMgr = editor("agents")
	configureSkillMgr = editor("skills")
	configureServerForm = editor("server")
	configureBrowserForm = editor("browser")
	configureSchedulerForm = editor("scheduler")
	configureProviderMgr = func(authpkg.Store) error {
		calls = append(calls, "providers")
		return editorErr
	}
	return &calls
}

func TestRunWizardOpensEachSectionUntilQuit(t *testing.T) {
	st, cfgPath := newConfigureTestStore(t)
	cfg := config.Default()
	calls := stubConfigureWizard(t, []configureMenuSelection{
		configureMenuGeneral, configureMenuProviders, configureMenuAgents, configureMenuSkills,
		configureMenuServer, configureMenuBrowser, configureMenuScheduler, configureMenuSelection(99),
		configureMenuQuit,
	}, nil)

	require.NoError(t, runWizard(&cfg, cfgPath, st))
	assert.Equal(t, []string{
		"general:" + cfgPath, "providers", "agents:" + cfgPath, "skills:" + cfgPath,
		"server:" + cfgPath, "browser:" + cfgPath, "scheduler:" + cfgPath,
	}, *calls)
}

func TestRunWizardReturnsEditorErrors(t *testing.T) {
	for _, sel := range []configureMenuSelection{
		configureMenuGeneral, configureMenuProviders, configureMenuAgents, configureMenuSkills,
		configureMenuServer, configureMenuBrowser, configureMenuScheduler,
	} {
		st, cfgPath := newConfigureTestStore(t)
		cfg := config.Default()
		wantErr := errors.New("editor failed")
		calls := stubConfigureWizard(t, []configureMenuSelection{sel}, wantErr)

		err := runWizard(&cfg, cfgPath, st)
		assert.ErrorIs(t, err, wantErr, "selection %d", sel)
		assert.Len(t, *calls, 1, "selection %d", sel)
	}
}

func TestRunWizardReturnsMenuError(t *testing.T) {
	st, cfgPath := newConfigureTestStore(t)
	cfg := config.Default()
	calls := stubConfigureWizard(t, nil, nil)
	wantErr := errors.New("menu failed")
	configureMenuRunner = func(*config.Config, authpkg.Store) (configureMenuSelection, error) {
		return configureMenuQuit, wantErr
	}

	assert.ErrorIs(t, runWizard(&cfg, cfgPath, st), wantErr)
	assert.Empty(t, *calls)
}
