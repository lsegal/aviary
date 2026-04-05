package cmd

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	authpkg "github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
)

type providerOption struct {
	name          string
	id            string
	apiKey        string
	oauthKey      string
	supportsOAuth bool
	requiresBase  bool
}

var tuiProviders = []providerOption{
	{name: "Anthropic", id: "anthropic", apiKey: "anthropic:default", oauthKey: "anthropic:oauth", supportsOAuth: true},
	{name: "OpenAI", id: "openai", apiKey: "openai:default", oauthKey: "openai:oauth", supportsOAuth: true},
	{name: "Gemini", id: "gemini", apiKey: "gemini:default", oauthKey: "gemini:oauth", supportsOAuth: true},
	{name: "vLLM", id: "vllm", apiKey: "vllm:default", requiresBase: true},
}

type providerMgrModel struct {
	store     authpkg.Store
	cfg       *config.Config
	cfgPath   string
	cursor    int
	mode      string
	method    int
	keyInput  textinput.Model
	codeInput textinput.Model
	baseInput textinput.Model
	message   string
	err       string
}

func newProviderMgrModel(cfg *config.Config, cfgPath string, st authpkg.Store) providerMgrModel {
	key := newInput("API key", "")
	key.EchoMode = textinput.EchoPassword
	code := newInput("Authorization code", "")
	base := newInput("Base URI", "")
	return providerMgrModel{store: st, cfg: cfg, cfgPath: cfgPath, keyInput: key, codeInput: code, baseInput: base}
}

func (m providerMgrModel) Init() tea.Cmd { return nil }
func (m providerMgrModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.mode {
		case "":
			return m.updateList(msg)
		case "method":
			return m.updateMethod(msg)
		case "apikey":
			return m.updateAPIKey(msg)
		case "oauth":
			return m.updateOAuth(msg)
		case "vllm":
			return m.updateVLLM(msg)
		}
	}
	return m, nil
}

func (m providerMgrModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(tuiProviders)-1 {
			m.cursor++
		}
	case "enter", " ":
		if tuiProviders[m.cursor].requiresBase {
			m.mode = "vllm"
			m.baseInput.SetValue(strings.TrimSpace(m.currentBaseURI()))
			m.keyInput.SetValue("")
			m.baseInput.Focus()
			return m, textinput.Blink
		}
		m.mode = "method"
		m.method = 0
	}
	return m, nil
}

func (m providerMgrModel) updateMethod(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := tuiProviders[m.cursor]
	maxMethod := 0
	if p.supportsOAuth {
		maxMethod = 1
	}
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = ""
	case "up", "k":
		if m.method > 0 {
			m.method--
		}
	case "down", "j":
		if m.method < maxMethod {
			m.method++
		}
	case "enter", " ":
		if m.method == 0 {
			m.mode = "apikey"
			m.keyInput.SetValue("")
			m.keyInput.Focus()
			return m, textinput.Blink
		}
		m.mode = "oauth"
		m.codeInput.SetValue("")
		m.codeInput.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m providerMgrModel) updateAPIKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.keyInput.Blur()
		m.mode = "method"
	case "enter":
		v := strings.TrimSpace(m.keyInput.Value())
		if v == "" {
			m.err = "API key cannot be empty"
			return m, nil
		}
		if err := m.store.Set(tuiProviders[m.cursor].apiKey, v); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.err = ""
		m.message = tuiProviders[m.cursor].name + " API key saved."
		m.mode = ""
	default:
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m providerMgrModel) updateOAuth(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.codeInput.Blur()
		m.mode = "method"
	case "enter":
		v := strings.TrimSpace(m.codeInput.Value())
		if v == "" {
			m.err = "OAuth token/code cannot be empty"
			return m, nil
		}
		if err := m.store.Set(tuiProviders[m.cursor].oauthKey, v); err != nil {
			m.err = err.Error()
			return m, nil
		}
		m.err = ""
		m.message = tuiProviders[m.cursor].name + " OAuth credential saved."
		m.mode = ""
	default:
		var cmd tea.Cmd
		m.codeInput, cmd = m.codeInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m providerMgrModel) updateVLLM(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.baseInput.Blur()
		m.keyInput.Blur()
		m.mode = ""
	case "tab", "shift+tab":
		if m.baseInput.Focused() {
			m.baseInput.Blur()
			m.keyInput.Focus()
		} else {
			m.keyInput.Blur()
			m.baseInput.Focus()
		}
		return m, nil
	case "enter":
		baseURI := strings.TrimSpace(m.baseInput.Value())
		if baseURI == "" {
			m.err = "Base URI cannot be empty"
			return m, nil
		}
		if err := m.saveProviderBaseURI("vllm", baseURI); err != nil {
			m.err = err.Error()
			return m, nil
		}
		if apiKey := strings.TrimSpace(m.keyInput.Value()); apiKey != "" {
			if err := m.store.Set(tuiProviders[m.cursor].apiKey, apiKey); err != nil {
				m.err = err.Error()
				return m, nil
			}
		}
		m.err = ""
		m.message = "vLLM endpoint saved."
		m.mode = ""
		return m, nil
	}
	if m.baseInput.Focused() {
		var cmd tea.Cmd
		m.baseInput, cmd = m.baseInput.Update(msg)
		return m, cmd
	}
	var cmd tea.Cmd
	m.keyInput, cmd = m.keyInput.Update(msg)
	return m, cmd
}

func (m providerMgrModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("Provider Authentication"))
	b.WriteString("\n")
	b.WriteString(tuiSectionStyle.Render(strings.Repeat("─", 60)))
	b.WriteString("\n")
	switch m.mode {
	case "":
		for i, p := range tuiProviders {
			state := "not connected"
			if p.requiresBase && strings.TrimSpace(m.currentBaseURI()) != "" {
				state = "endpoint"
				if _, err := m.store.Get(p.apiKey); err == nil {
					state = "endpoint + api key"
				}
			} else if _, err := m.store.Get(p.oauthKey); err == nil {
				state = "oauth"
			} else if _, err := m.store.Get(p.apiKey); err == nil {
				state = "api key"
			}
			name := fmtPadRight(p.name, 12)
			if i == m.cursor {
				name = tuiSelectedStyle.Render(name)
			}
			b.WriteString(tuiCursor(i == m.cursor) + " " + name + "  " + state + "\n")
		}
		b.WriteString("\n" + tuiHelpStyle.Render("Enter configure · ↑/↓ navigate · Esc/q quit"))
	case "method":
		opts := []string{"API key", "OAuth"}
		b.WriteString(tuiLabelStyle.Render("Connect "+tuiProviders[m.cursor].name) + "\n\n")
		limit := 1
		if !tuiProviders[m.cursor].supportsOAuth {
			limit = 0
		}
		for i := 0; i <= limit; i++ {
			line := opts[i]
			if i == m.method {
				line = tuiSelectedStyle.Render(line)
			}
			b.WriteString(tuiCursor(i == m.method) + " " + line + "\n")
		}
		b.WriteString("\n" + tuiHelpStyle.Render("Enter select · Esc back"))
	case "apikey":
		b.WriteString(tuiLabelStyle.Render(tuiProviders[m.cursor].name+" API Key") + "\n\n")
		b.WriteString(m.keyInput.View())
		b.WriteString("\n\n" + tuiHelpStyle.Render("Enter save · Esc back"))
	case "oauth":
		b.WriteString(tuiLabelStyle.Render(tuiProviders[m.cursor].name+" OAuth") + "\n")
		b.WriteString(tuiDimStyle.Render("Paste an OAuth token/code."))
		b.WriteString("\n\n")
		b.WriteString(m.codeInput.View())
		b.WriteString("\n\n" + tuiHelpStyle.Render("Enter save · Esc back"))
	case "vllm":
		b.WriteString(tuiLabelStyle.Render("Configure vLLM") + "\n")
		b.WriteString(tuiDimStyle.Render("Set the OpenAI-compatible base URI. API key is optional."))
		b.WriteString("\n\n")
		b.WriteString(m.baseInput.View())
		b.WriteString("\n\n")
		b.WriteString(m.keyInput.View())
		b.WriteString("\n\n" + tuiHelpStyle.Render("Tab switch field · Enter save · Esc back"))
	}
	if m.message != "" {
		b.WriteString("\n" + tuiSuccessStyle.Render(m.message))
	}
	if m.err != "" {
		b.WriteString("\n" + tuiErrorStyle.Render(m.err))
	}
	return b.String()
}

func runProviderMgr(st authpkg.Store) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		d := config.Default()
		cfg = &d
	}
	_, err = tea.NewProgram(newProviderMgrModel(cfg, cfgFile, st), tea.WithAltScreen()).Run()
	return err
}

func (m providerMgrModel) currentBaseURI() string {
	if m.cfg == nil || m.cfg.Models.Providers == nil {
		return ""
	}
	return m.cfg.Models.Providers["vllm"].BaseURI
}

func (m providerMgrModel) saveProviderBaseURI(provider, baseURI string) error {
	if m.cfg == nil {
		d := config.Default()
		m.cfg = &d
	}
	if m.cfg.Models.Providers == nil {
		m.cfg.Models.Providers = map[string]config.ProviderConfig{}
	}
	pc := m.cfg.Models.Providers[provider]
	pc.BaseURI = strings.TrimSpace(baseURI)
	if provider == "vllm" && strings.TrimSpace(pc.Auth) == "" {
		pc.Auth = "auth:vllm:default"
	}
	m.cfg.Models.Providers[provider] = pc
	return config.Save(m.cfgPath, m.cfg)
}

func fmtPadRight(v string, width int) string {
	if len(v) >= width {
		return v
	}
	return v + strings.Repeat(" ", width-len(v))
}
