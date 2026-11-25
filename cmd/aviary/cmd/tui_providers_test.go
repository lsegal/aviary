package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
)

type providerTestStore struct {
	data   map[string]string
	setErr error
}

func newProviderTestStore() *providerTestStore {
	return &providerTestStore{data: map[string]string{}}
}

func (s *providerTestStore) Set(key, value string) error {
	if s.setErr != nil {
		return s.setErr
	}
	s.data[key] = value
	return nil
}

func (s *providerTestStore) Get(key string) (string, error) {
	v, ok := s.data[key]
	if !ok {
		return "", &authpkg.ErrNotFound{Key: key}
	}
	return v, nil
}

func (s *providerTestStore) Delete(key string) error {
	delete(s.data, key)
	return nil
}

func (s *providerTestStore) List() ([]string, error) {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func providerKey(s string) tea.KeyMsg {
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
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func sendProviderKeys(t *testing.T, m providerMgrModel, keys ...string) (providerMgrModel, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var next tea.Model
		next, cmd = m.Update(providerKey(k))
		m = next.(providerMgrModel)
	}
	return m, cmd
}

func typeProviderText(t *testing.T, m providerMgrModel, text string) providerMgrModel {
	t.Helper()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return next.(providerMgrModel)
}

func providerIndex(t *testing.T, id string) int {
	t.Helper()
	for i, p := range tuiProviders {
		if p.id == id {
			return i
		}
	}
	t.Fatalf("provider %q not found", id)
	return -1
}

func newTestProviderMgr(t *testing.T) (providerMgrModel, *providerTestStore, string) {
	t.Helper()
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "aviary.yaml")
	st := newProviderTestStore()
	return newProviderMgrModel(&cfg, cfgPath, st), st, cfgPath
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestProviderMgrInitAndNonKeyMsg(t *testing.T) {
	m, _, _ := newTestProviderMgr(t)
	assert.Nil(t, m.Init())
	next, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	assert.Nil(t, cmd)
	assert.Equal(t, m.mode, next.(providerMgrModel).mode)
}

func TestProviderMgrListNavigationAndQuit(t *testing.T) {
	m, _, _ := newTestProviderMgr(t)

	m, _ = sendProviderKeys(t, m, "up")
	assert.Equal(t, 0, m.cursor, "cursor clamps at top")

	m, _ = sendProviderKeys(t, m, "down", "j")
	assert.Equal(t, 2, m.cursor)
	m, _ = sendProviderKeys(t, m, "k")
	assert.Equal(t, 1, m.cursor)

	for range tuiProviders {
		m, _ = sendProviderKeys(t, m, "down")
	}
	assert.Equal(t, len(tuiProviders)-1, m.cursor, "cursor clamps at bottom")

	for _, k := range []string{"q", "esc", "ctrl+c"} {
		_, cmd := sendProviderKeys(t, m, k)
		assert.True(t, isQuit(cmd), "key %q should quit", k)
	}
}

func TestProviderMgrAPIKeyFlow(t *testing.T) {
	m, st, _ := newTestProviderMgr(t)

	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "method", m.mode)
	assert.Equal(t, 0, m.method)
	assert.Contains(t, m.View(), "Connect Anthropic")
	assert.Contains(t, m.View(), "OAuth")

	m, cmd := sendProviderKeys(t, m, "enter")
	assert.Equal(t, "apikey", m.mode)
	assert.NotNil(t, cmd)
	assert.True(t, m.keyInput.Focused())
	assert.Contains(t, m.View(), "Anthropic API Key")

	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "apikey", m.mode)
	assert.Equal(t, "API key cannot be empty", m.err)
	assert.Contains(t, m.View(), "API key cannot be empty")

	m = typeProviderText(t, m, "  sk-test  ")
	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "", m.mode)
	assert.Equal(t, "", m.err)
	assert.Equal(t, "Anthropic API key saved.", m.message)
	assert.Equal(t, "sk-test", st.data["anthropic:default"])

	view := m.View()
	assert.Contains(t, view, "Anthropic API key saved.")
	assert.Regexp(t, `Anthropic\s+api key`, view)
	assert.Regexp(t, `OpenAI\s+not connected`, view)
}

func TestProviderMgrAPIKeyStoreErrorAndEsc(t *testing.T) {
	m, st, _ := newTestProviderMgr(t)
	st.setErr = errors.New("keychain locked")

	m, _ = sendProviderKeys(t, m, "enter", "enter")
	m = typeProviderText(t, m, "sk-test")
	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "apikey", m.mode)
	assert.Equal(t, "keychain locked", m.err)
	assert.Empty(t, st.data)

	m, _ = sendProviderKeys(t, m, "esc")
	assert.Equal(t, "method", m.mode)
	assert.False(t, m.keyInput.Focused())

	m, _ = sendProviderKeys(t, m, "enter")
	_, cmd := sendProviderKeys(t, m, "ctrl+c")
	assert.True(t, isQuit(cmd))
}

func TestProviderMgrOAuthFlow(t *testing.T) {
	m, st, _ := newTestProviderMgr(t)
	m.cursor = providerIndex(t, "openai")

	m, _ = sendProviderKeys(t, m, " ")
	assert.Equal(t, "method", m.mode)

	m, _ = sendProviderKeys(t, m, "down", "j")
	assert.Equal(t, 1, m.method, "method clamps at OAuth")
	m, _ = sendProviderKeys(t, m, "up", "k")
	assert.Equal(t, 0, m.method, "method clamps at API key")

	m, _ = sendProviderKeys(t, m, "down", "enter")
	assert.Equal(t, "oauth", m.mode)
	assert.True(t, m.codeInput.Focused())
	view := m.View()
	assert.Contains(t, view, "OpenAI OAuth")
	assert.Contains(t, view, "Paste an OAuth token/code.")

	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "OAuth token/code cannot be empty", m.err)

	m = typeProviderText(t, m, "oauth-code")
	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "", m.mode)
	assert.Equal(t, "OpenAI OAuth credential saved.", m.message)
	assert.Equal(t, "oauth-code", st.data["openai:oauth"])
	assert.Regexp(t, `OpenAI\s+oauth`, m.View())
}

func TestProviderMgrOAuthStoreErrorEscAndQuit(t *testing.T) {
	m, st, _ := newTestProviderMgr(t)
	m.cursor = providerIndex(t, "gemini")
	st.setErr = errors.New("write failed")

	m, _ = sendProviderKeys(t, m, "enter", "down", "enter")
	m = typeProviderText(t, m, "code")
	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "oauth", m.mode)
	assert.Equal(t, "write failed", m.err)

	_, cmd := sendProviderKeys(t, m, "ctrl+c")
	assert.True(t, isQuit(cmd))

	m, _ = sendProviderKeys(t, m, "esc")
	assert.Equal(t, "method", m.mode)
	assert.False(t, m.codeInput.Focused())

	m, _ = sendProviderKeys(t, m, "esc")
	assert.Equal(t, "", m.mode)
}

func TestProviderMgrMethodWithoutOAuth(t *testing.T) {
	m, _, _ := newTestProviderMgr(t)
	m.cursor = providerIndex(t, "meta")

	m, _ = sendProviderKeys(t, m, "enter", "down")
	assert.Equal(t, 0, m.method, "providers without OAuth only offer API key")
	view := m.View()
	assert.Contains(t, view, "Connect Meta (Muse)")
	assert.NotContains(t, view, "OAuth")

	_, cmd := sendProviderKeys(t, m, "ctrl+c")
	assert.True(t, isQuit(cmd))
}

func TestProviderMgrEndpointFlow(t *testing.T) {
	m, st, cfgPath := newTestProviderMgr(t)
	m.cfg.Models.Providers = map[string]config.ProviderConfig{
		"vllm": {BaseURI: " http://old:8000/v1 "},
	}
	m.cursor = providerIndex(t, "vllm")
	m.keyInput.SetValue("stale")

	m, cmd := sendProviderKeys(t, m, "enter")
	assert.Equal(t, "endpoint", m.mode)
	assert.NotNil(t, cmd)
	assert.Equal(t, "http://old:8000/v1", m.baseInput.Value())
	assert.Equal(t, "", m.keyInput.Value())
	assert.True(t, m.baseInput.Focused())
	view := m.View()
	assert.Contains(t, view, "Configure vLLM")
	assert.Contains(t, view, "Tab switch field")

	m.baseInput.SetValue("")
	m = typeProviderText(t, m, "http://gpu:8000/v1")
	assert.Equal(t, "http://gpu:8000/v1", m.baseInput.Value())

	m, _ = sendProviderKeys(t, m, "tab")
	assert.False(t, m.baseInput.Focused())
	assert.True(t, m.keyInput.Focused())
	m = typeProviderText(t, m, "vllm-key")
	assert.Equal(t, "vllm-key", m.keyInput.Value())
	assert.Equal(t, "http://gpu:8000/v1", m.baseInput.Value())

	m, _ = sendProviderKeys(t, m, "tab")
	assert.True(t, m.baseInput.Focused())
	assert.False(t, m.keyInput.Focused())

	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "", m.mode)
	assert.Equal(t, "", m.err)
	assert.Equal(t, "vLLM endpoint saved.", m.message)
	assert.Equal(t, "vllm-key", st.data["vllm:default"])

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "http://gpu:8000/v1", loaded.Models.Providers["vllm"].BaseURI)
	assert.Equal(t, "auth:vllm:default", loaded.Models.Providers["vllm"].Auth)

	assert.Regexp(t, `vLLM\s+endpoint \+ api key`, m.View())
}

func TestProviderMgrEndpointWithoutKeyEscAndQuit(t *testing.T) {
	m, st, cfgPath := newTestProviderMgr(t)
	m.cfg.Models.Providers = nil
	m.cursor = providerIndex(t, "ollama")

	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "", m.baseInput.Value())

	_, cmd := sendProviderKeys(t, m, "ctrl+c")
	assert.True(t, isQuit(cmd))

	m, _ = sendProviderKeys(t, m, "esc")
	assert.Equal(t, "", m.mode)
	assert.False(t, m.baseInput.Focused())
	assert.False(t, m.keyInput.Focused())

	m, _ = sendProviderKeys(t, m, "enter")
	m = typeProviderText(t, m, "http://localhost:11434")
	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "Ollama endpoint saved.", m.message)
	assert.Empty(t, st.data, "blank API key is not stored")

	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:11434", loaded.Models.Providers["ollama"].BaseURI)
	assert.Regexp(t, `Ollama\s+endpoint\n`, m.View())
}

func TestProviderMgrEndpointErrors(t *testing.T) {
	m, st, _ := newTestProviderMgr(t)
	m.cursor = providerIndex(t, "vllm")

	m, _ = sendProviderKeys(t, m, "enter", "tab")
	m = typeProviderText(t, m, "key")
	st.setErr = errors.New("store failed")
	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "endpoint", m.mode)
	assert.Equal(t, "store failed", m.err)

	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	m.cfgPath = filepath.Join(blocker, "aviary.yaml")
	m, _ = sendProviderKeys(t, m, "enter")
	assert.Equal(t, "endpoint", m.mode)
	assert.Contains(t, m.err, "creating config dir")
}

func TestProviderMgrCurrentBaseURI(t *testing.T) {
	m, _, _ := newTestProviderMgr(t)
	m.cfg.Models.Providers = map[string]config.ProviderConfig{"vllm": {BaseURI: "http://x"}}
	assert.Equal(t, "http://x", m.currentBaseURI("vllm"))
	assert.Equal(t, "", m.currentBaseURI("ollama"))

	m.cfg.Models.Providers = nil
	assert.Equal(t, "", m.currentBaseURI("vllm"))

	m.cfg = nil
	assert.Equal(t, "", m.currentBaseURI("vllm"))
	assert.Contains(t, m.View(), "not connected")
}

func TestProviderMgrSaveProviderBaseURI(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "aviary.yaml")
	m := newProviderMgrModel(nil, cfgPath, newProviderTestStore())
	require.NoError(t, m.saveProviderBaseURI("vllm", "  http://nil-cfg  "))
	loaded, err := config.Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "http://nil-cfg", loaded.Models.Providers["vllm"].BaseURI)
	assert.Equal(t, "auth:vllm:default", loaded.Models.Providers["vllm"].Auth)

	cfg := config.Default()
	cfg.Models.Providers = map[string]config.ProviderConfig{"ollama": {Auth: "auth:custom"}}
	m = newProviderMgrModel(&cfg, cfgPath, newProviderTestStore())
	require.NoError(t, m.saveProviderBaseURI("ollama", "http://o"))
	assert.Equal(t, "auth:custom", cfg.Models.Providers["ollama"].Auth, "existing auth is preserved")
	assert.Equal(t, "http://o", cfg.Models.Providers["ollama"].BaseURI)
}

func TestFmtPadRight(t *testing.T) {
	assert.Equal(t, "ab   ", fmtPadRight("ab", 5))
	assert.Equal(t, "abcde", fmtPadRight("abcde", 5))
	assert.Equal(t, "abcdef", fmtPadRight("abcdef", 3))
}
