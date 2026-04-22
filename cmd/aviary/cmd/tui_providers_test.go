package cmd

import (
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bedrockCursorIndex() int {
	for i, p := range tuiProviders {
		if p.id == "bedrock" {
			return i
		}
	}
	return -1
}

func testKeyMsg(key string) tea.KeyMsg {
	switch key {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func newTestProviderMgr(t *testing.T) (providerMgrModel, string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	store.SetDataDir(filepath.Join(base, "aviary"))
	t.Cleanup(func() { store.SetDataDir("") })
	require.NoError(t, store.EnsureDirs())

	cfgPath := filepath.Join(base, "aviary", "aviary.yaml")
	cfg := config.Default()
	st := authStore()
	return newProviderMgrModel(&cfg, cfgPath, st), cfgPath
}

func TestBedrockTUI_ProviderOptionIsBedrock(t *testing.T) {
	idx := bedrockCursorIndex()
	require.NotEqual(t, -1, idx)
	p := tuiProviders[idx]
	assert.True(t, p.isBedrock)
	assert.False(t, p.requiresBase)
	assert.False(t, p.supportsOAuth)
}

func TestBedrockTUI_EnterBedrockMode(t *testing.T) {
	m, _ := newTestProviderMgr(t)
	m.cursor = bedrockCursorIndex()

	got, _ := m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)
	assert.Equal(t, "bedrock", m.mode)
	assert.True(t, m.regionInput.Focused())
}

func TestBedrockTUI_SaveRegionOnly(t *testing.T) {
	m, cfgPath := newTestProviderMgr(t)
	m.cursor = bedrockCursorIndex()

	got, _ := m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)
	require.Equal(t, "bedrock", m.mode)

	m.regionInput.SetValue("eu-west-1")
	got, _ = m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)

	assert.Equal(t, "", m.mode)
	assert.Equal(t, "Bedrock configuration saved.", m.message)
	assert.Equal(t, "", m.err)

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "eu-west-1", loaded.Models.Providers["bedrock"].Region)
	assert.Equal(t, "auth:bedrock:default", loaded.Models.Providers["bedrock"].Auth)
}

func TestBedrockTUI_SaveRegionAndCredentials(t *testing.T) {
	m, cfgPath := newTestProviderMgr(t)
	m.cursor = bedrockCursorIndex()

	got, _ := m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)

	m.regionInput.SetValue("us-west-2")
	m.keyInput.SetValue("AKIAEXAMPLE")
	m.secretInput.SetValue("wJalrXUtnFEMI/SECRET")

	got, _ = m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)

	assert.Equal(t, "", m.mode)
	assert.Equal(t, "Bedrock configuration saved.", m.message)

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "us-west-2", loaded.Models.Providers["bedrock"].Region)

	val, err := m.store.Get("bedrock:default")
	require.NoError(t, err)
	assert.Equal(t, "AKIAEXAMPLE:wJalrXUtnFEMI/SECRET", val)
}

func TestBedrockTUI_EmptyRegionShowsError(t *testing.T) {
	m, _ := newTestProviderMgr(t)
	m.cursor = bedrockCursorIndex()

	got, _ := m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)
	require.Equal(t, "bedrock", m.mode)

	m.regionInput.SetValue("")
	got, _ = m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)

	assert.Equal(t, "bedrock", m.mode, "should stay in bedrock mode on error")
	assert.Equal(t, "AWS region cannot be empty", m.err)
}

func TestBedrockTUI_EscReturnsToList(t *testing.T) {
	m, _ := newTestProviderMgr(t)
	m.cursor = bedrockCursorIndex()

	got, _ := m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)
	require.Equal(t, "bedrock", m.mode)

	got, _ = m.Update(testKeyMsg("esc"))
	m = got.(providerMgrModel)
	assert.Equal(t, "", m.mode)
}

func TestBedrockTUI_TabCyclesFields(t *testing.T) {
	m, _ := newTestProviderMgr(t)
	m.cursor = bedrockCursorIndex()

	got, _ := m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)
	assert.True(t, m.regionInput.Focused(), "region should be focused initially")

	got, _ = m.Update(testKeyMsg("tab"))
	m = got.(providerMgrModel)
	assert.False(t, m.regionInput.Focused())
	assert.True(t, m.profileInput.Focused(), "profile should be focused after first tab")

	got, _ = m.Update(testKeyMsg("tab"))
	m = got.(providerMgrModel)
	assert.False(t, m.profileInput.Focused())
	assert.True(t, m.keyInput.Focused(), "access key should be focused after second tab")

	got, _ = m.Update(testKeyMsg("tab"))
	m = got.(providerMgrModel)
	assert.False(t, m.keyInput.Focused())
	assert.True(t, m.secretInput.Focused(), "secret key should be focused after third tab")

	got, _ = m.Update(testKeyMsg("tab"))
	m = got.(providerMgrModel)
	assert.False(t, m.secretInput.Focused())
	assert.True(t, m.regionInput.Focused(), "should cycle back to region")
}

func TestBedrockTUI_ViewListShowsRegion(t *testing.T) {
	m, _ := newTestProviderMgr(t)
	m.cursor = bedrockCursorIndex()

	view := m.View()
	assert.Contains(t, view, "Bedrock")
	assert.Contains(t, view, "not connected")

	m.cfg.Models.Providers = map[string]config.ProviderConfig{
		"bedrock": {Region: "us-east-1", Auth: "auth:bedrock:default"},
	}
	view = m.View()
	assert.Contains(t, view, "region: us-east-1")
}

func TestBedrockTUI_ViewBedrockMode(t *testing.T) {
	m, _ := newTestProviderMgr(t)
	m.cursor = bedrockCursorIndex()
	m.mode = "bedrock"
	m.regionInput.Focus()

	view := m.View()
	assert.Contains(t, view, "Configure Bedrock")
	assert.Contains(t, view, "AWS region")
	assert.Contains(t, view, "Access key")
	assert.Contains(t, view, "Secret access key")
	assert.Contains(t, view, "Tab switch field")
}

func TestBedrockTUI_PreloadsExistingRegion(t *testing.T) {
	m, _ := newTestProviderMgr(t)
	m.cfg.Models.Providers = map[string]config.ProviderConfig{
		"bedrock": {Region: "ap-southeast-1", Auth: "auth:bedrock:default"},
	}
	m.cursor = bedrockCursorIndex()

	got, _ := m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)

	assert.Equal(t, "bedrock", m.mode)
	assert.Equal(t, "ap-southeast-1", m.regionInput.Value())
}

func TestBedrockTUI_PartialCredentialsNotSaved(t *testing.T) {
	m, cfgPath := newTestProviderMgr(t)
	m.cursor = bedrockCursorIndex()

	got, _ := m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)

	m.regionInput.SetValue("us-east-1")
	m.keyInput.SetValue("AKIAEXAMPLE")
	// secret intentionally left empty

	got, _ = m.Update(testKeyMsg("enter"))
	m = got.(providerMgrModel)

	assert.Equal(t, "", m.mode)
	assert.Equal(t, "Bedrock configuration saved.", m.message)

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", loaded.Models.Providers["bedrock"].Region)

	_, err = m.store.Get("bedrock:default")
	assert.Error(t, err, "partial credentials should not be saved")
}
