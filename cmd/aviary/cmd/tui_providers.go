package cmd

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	authpkg "github.com/lsegal/aviary/internal/auth"
)

type providerOption struct {
	name          string
	apiKey        string
	oauthKey      string
	supportsOAuth bool
}

var tuiProviders = []providerOption{
	{name: "Anthropic", apiKey: "anthropic:default", oauthKey: "anthropic:oauth", supportsOAuth: true},
	{name: "OpenAI", apiKey: "openai:default", oauthKey: "openai:oauth", supportsOAuth: true},
	{name: "Gemini", apiKey: "gemini:default", oauthKey: "gemini:oauth", supportsOAuth: true},
}

type providerMgrModel struct {
	store     authpkg.Store
	cursor    int
	mode      string
	method    int
	keyInput  textinput.Model
	codeInput textinput.Model
	message   string
	err       string
}

func newProviderMgrModel(st authpkg.Store) providerMgrModel {
	key := newInput("API key", "")
	key.EchoMode = textinput.EchoPassword
	code := newInput("Authorization code", "")
	return providerMgrModel{store: st, keyInput: key, codeInput: code}
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
			if _, err := m.store.Get(p.oauthKey); err == nil {
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
	_, err := tea.NewProgram(newProviderMgrModel(st), tea.WithAltScreen()).Run()
	return err
}

func fmtPadRight(v string, width int) string {
	if len(v) >= width {
		return v
	}
	return v + strings.Repeat(" ", width-len(v))
}
