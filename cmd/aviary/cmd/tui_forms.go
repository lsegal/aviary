package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lsegal/aviary/internal/config"
)

type savedMsg struct{}
type errMsg string

type simpleFormField struct {
	label  string
	desc   string
	input  textinput.Model
	isBool bool
	value  bool
}

type generalFormModel struct {
	fields  []simpleFormField
	cursor  int
	cfg     *config.Config
	cfgPath string
	width   int
	saved   bool
	err     string
}

func newInput(placeholder, value string) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.SetValue(value)
	in.Width = 48
	return in
}

func clampInputWidth(totalWidth int) int {
	if totalWidth <= 0 {
		return 48
	}
	width := totalWidth - 12
	if width < 16 {
		return 16
	}
	if width > 72 {
		return 72
	}
	return width
}

func renderSimpleFields(fields []simpleFormField, cursor int) string {
	var b strings.Builder
	for i, f := range fields {
		prefix := "  "
		if i == cursor {
			prefix = tuiSelectedStyle.Render("> ")
		}
		label := f.label
		if i == cursor {
			label = tuiSelectedStyle.Render(label)
		}
		if f.isBool {
			value := "[off]"
			if f.value {
				value = "[on] "
			}
			if f.value {
				value = tuiSuccessStyle.Render(value)
			} else {
				value = tuiDimStyle.Render(value)
			}
			b.WriteString(prefix + label + "  " + value)
			if f.desc != "" {
				b.WriteString("  " + tuiDimStyle.Render(f.desc))
			}
			b.WriteString("\n")
			continue
		}
		b.WriteString(prefix + label)
		if f.desc != "" {
			b.WriteString("  " + tuiDimStyle.Render(f.desc))
		}
		b.WriteString("\n    " + f.input.View() + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func newGeneralFormModel(cfg *config.Config, cfgPath string) generalFormModel {
	port := "16677"
	if cfg.Server.Port > 0 {
		port = strconv.Itoa(cfg.Server.Port)
	}
	cdp := ""
	if cfg.Browser.CDPPort > 0 {
		cdp = strconv.Itoa(cfg.Browser.CDPPort)
	}
	concurrency := ""
	if cfg.Scheduler.Concurrency != nil {
		concurrency = fmt.Sprintf("%v", cfg.Scheduler.Concurrency)
	}
	fields := []simpleFormField{
		{label: "Port", desc: "HTTP(S) listen port", input: newInput("16677", port)},
		{label: "External access", desc: "Bind to 0.0.0.0", isBool: true, value: cfg.Server.ExternalAccess},
		{label: "Disable TLS", desc: "Use plain HTTP", isBool: true, value: cfg.Server.NoTLS},
		{label: "Browser binary", desc: "Leave empty to auto-detect", input: newInput("auto-detected", cfg.Browser.Binary)},
		{label: "CDP port", desc: "Chrome DevTools Protocol port", input: newInput("9222", cdp)},
		{label: "Concurrency", desc: "Task workers or 'auto'", input: newInput("auto", concurrency)},
		{label: "Brave key ref", desc: "Exact config value for search.web.brave_api_key", input: newInput("auth:brave_api_key", strings.TrimSpace(cfg.Search.Web.BraveAPIKey))},
	}
	fields[0].input.Focus()
	return generalFormModel{fields: fields, cfg: cfg, cfgPath: cfgPath}
}

func (m generalFormModel) Init() tea.Cmd { return textinput.Blink }
func (m generalFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		for i := range m.fields {
			if !m.fields[i].isBool {
				m.fields[i].input.Width = clampInputWidth(m.width)
			}
		}
	case savedMsg:
		m.saved = true
	case errMsg:
		m.err = string(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "up", "k", "shift+tab":
			if m.cursor > 0 {
				if !m.fields[m.cursor].isBool {
					m.fields[m.cursor].input.Blur()
				}
				m.cursor--
				if !m.fields[m.cursor].isBool {
					m.fields[m.cursor].input.Focus()
				}
			}
			return m, nil
		case "down", "j", "tab":
			if m.cursor < len(m.fields)-1 {
				if !m.fields[m.cursor].isBool {
					m.fields[m.cursor].input.Blur()
				}
				m.cursor++
				if !m.fields[m.cursor].isBool {
					m.fields[m.cursor].input.Focus()
				}
			}
			return m, nil
		case " ":
			if m.fields[m.cursor].isBool {
				m.fields[m.cursor].value = !m.fields[m.cursor].value
				return m, nil
			}
		case "ctrl+s":
			return m, m.saveCmd()
		}
	}
	if !m.fields[m.cursor].isBool {
		var cmd tea.Cmd
		m.fields[m.cursor].input, cmd = m.fields[m.cursor].input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m generalFormModel) saveCmd() tea.Cmd {
	cfg := m.cfg
	cfgPath := m.cfgPath
	return func() tea.Msg {
		portStr := strings.TrimSpace(m.fields[0].input.Value())
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			cfg.Server.Port = p
		}
		cfg.Server.ExternalAccess = m.fields[1].value
		cfg.Server.NoTLS = m.fields[2].value
		cfg.Browser.Binary = strings.TrimSpace(m.fields[3].input.Value())
		if p, err := strconv.Atoi(strings.TrimSpace(m.fields[4].input.Value())); err == nil {
			cfg.Browser.CDPPort = p
		}
		concurrency := strings.TrimSpace(m.fields[5].input.Value())
		switch concurrency {
		case "", "auto":
			cfg.Scheduler.Concurrency = nil
		default:
			p, err := strconv.Atoi(concurrency)
			if err != nil {
				return errMsg("Concurrency must be a number or 'auto'")
			}
			cfg.Scheduler.Concurrency = p
		}
		cfg.Search.Web.BraveAPIKey = strings.TrimSpace(m.fields[6].input.Value())
		if err := config.Save(cfgPath, cfg); err != nil {
			return errMsg(err.Error())
		}
		return savedMsg{}
	}
}

func (m generalFormModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("General Settings") + "\n")
	b.WriteString(tuiSectionStyle.Render(strings.Repeat("─", maxInt(24, minInt(72, widthOrDefault(m.width)-2)))))
	b.WriteString("\n")
	b.WriteString(renderSimpleFields(m.fields, m.cursor))
	if m.saved {
		b.WriteString("\n" + tuiSuccessStyle.Render("Saved."))
	}
	if m.err != "" {
		b.WriteString("\n" + tuiErrorStyle.Render(m.err))
	}
	b.WriteString("\n\n" + tuiHelpStyle.Render("Tab/↑/↓ navigate · Space toggle · Ctrl+S save · Esc/q quit"))
	return b.String()
}

type serverFormModel struct {
	fields  []simpleFormField
	cursor  int
	cfg     *config.Config
	cfgPath string
	width   int
	saved   bool
	err     string
}

func newServerFormModel(cfg *config.Config, cfgPath string) serverFormModel {
	port := "16677"
	if cfg.Server.Port > 0 {
		port = strconv.Itoa(cfg.Server.Port)
	}
	fields := []simpleFormField{
		{label: "Port", desc: "HTTP(S) listen port", input: newInput("16677", port)},
		{label: "External access", desc: "Bind to 0.0.0.0", isBool: true, value: cfg.Server.ExternalAccess},
		{label: "Disable TLS", desc: "Use plain HTTP", isBool: true, value: cfg.Server.NoTLS},
	}
	fields[0].input.Focus()
	return serverFormModel{fields: fields, cfg: cfg, cfgPath: cfgPath}
}

func (m serverFormModel) Init() tea.Cmd { return textinput.Blink }
func (m serverFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		for i := range m.fields {
			if !m.fields[i].isBool {
				m.fields[i].input.Width = clampInputWidth(m.width)
			}
		}
	case savedMsg:
		m.saved = true
	case errMsg:
		m.err = string(msg)
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}
func (m serverFormModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k", "shift+tab":
		if m.cursor > 0 {
			if !m.fields[m.cursor].isBool {
				m.fields[m.cursor].input.Blur()
			}
			m.cursor--
			if !m.fields[m.cursor].isBool {
				m.fields[m.cursor].input.Focus()
			}
		}
	case "down", "j", "tab":
		if m.cursor < len(m.fields)-1 {
			if !m.fields[m.cursor].isBool {
				m.fields[m.cursor].input.Blur()
			}
			m.cursor++
			if !m.fields[m.cursor].isBool {
				m.fields[m.cursor].input.Focus()
			}
		}
	case " ", "enter":
		if m.fields[m.cursor].isBool {
			m.fields[m.cursor].value = !m.fields[m.cursor].value
			return m, nil
		}
		if msg.String() == "enter" && m.cursor == len(m.fields)-1 {
			return m, m.saveCmd()
		}
	case "ctrl+s":
		return m, m.saveCmd()
	}
	if !m.fields[m.cursor].isBool {
		var cmd tea.Cmd
		m.fields[m.cursor].input, cmd = m.fields[m.cursor].input.Update(msg)
		return m, cmd
	}
	return m, nil
}
func (m serverFormModel) saveCmd() tea.Cmd {
	cfg := m.cfg
	cfgPath := m.cfgPath
	portStr := strings.TrimSpace(m.fields[0].input.Value())
	external := m.fields[1].value
	noTLS := m.fields[2].value
	return func() tea.Msg {
		if p, err := strconv.Atoi(portStr); err == nil && p > 0 {
			cfg.Server.Port = p
		}
		cfg.Server.ExternalAccess = external
		cfg.Server.NoTLS = noTLS
		if err := config.Save(cfgPath, cfg); err != nil {
			return errMsg(err.Error())
		}
		return savedMsg{}
	}
}
func (m serverFormModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("Server Settings") + "\n")
	b.WriteString(tuiSectionStyle.Render(strings.Repeat("─", maxInt(24, minInt(72, widthOrDefault(m.width)-2)))))
	b.WriteString("\n")
	b.WriteString(renderSimpleFields(m.fields, m.cursor))
	if m.saved {
		b.WriteString("\n" + tuiSuccessStyle.Render("Saved."))
	}
	if m.err != "" {
		b.WriteString("\n" + tuiErrorStyle.Render(m.err))
	}
	b.WriteString("\n\n" + tuiHelpStyle.Render("Tab/↑/↓ navigate · Space toggle · Ctrl+S save · Esc/q quit"))
	return b.String()
}

type browserFormModel struct {
	fields  []simpleFormField
	cursor  int
	cfg     *config.Config
	cfgPath string
	width   int
	saved   bool
	err     string
}

func newBrowserFormModel(cfg *config.Config, cfgPath string) browserFormModel {
	cdp := ""
	if cfg.Browser.CDPPort > 0 {
		cdp = strconv.Itoa(cfg.Browser.CDPPort)
	}
	fields := []simpleFormField{
		{label: "Profile directory", desc: "Chrome profile dir", input: newInput("Aviary", cfg.Browser.ProfileDir)},
		{label: "Browser binary", desc: "Leave empty to auto-detect", input: newInput("auto-detected", cfg.Browser.Binary)},
		{label: "CDP port", desc: "Chrome DevTools Protocol port", input: newInput("9222", cdp)},
		{label: "Headless mode", desc: "Run without visible window", isBool: true, value: cfg.Browser.Headless},
	}
	fields[0].input.Focus()
	return browserFormModel{fields: fields, cfg: cfg, cfgPath: cfgPath}
}
func (m browserFormModel) Init() tea.Cmd { return textinput.Blink }
func (m browserFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		for i := range m.fields {
			if !m.fields[i].isBool {
				m.fields[i].input.Width = clampInputWidth(m.width)
			}
		}
	case savedMsg:
		m.saved = true
	case errMsg:
		m.err = string(msg)
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	return m, nil
}
func (m browserFormModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k", "shift+tab":
		if m.cursor > 0 {
			if !m.fields[m.cursor].isBool {
				m.fields[m.cursor].input.Blur()
			}
			m.cursor--
			if !m.fields[m.cursor].isBool {
				m.fields[m.cursor].input.Focus()
			}
		}
	case "down", "j", "tab":
		if m.cursor < len(m.fields)-1 {
			if !m.fields[m.cursor].isBool {
				m.fields[m.cursor].input.Blur()
			}
			m.cursor++
			if !m.fields[m.cursor].isBool {
				m.fields[m.cursor].input.Focus()
			}
		}
	case " ", "enter":
		if m.fields[m.cursor].isBool {
			m.fields[m.cursor].value = !m.fields[m.cursor].value
			return m, nil
		}
	case "ctrl+s":
		return m, m.saveCmd()
	}
	if !m.fields[m.cursor].isBool {
		var cmd tea.Cmd
		m.fields[m.cursor].input, cmd = m.fields[m.cursor].input.Update(msg)
		return m, cmd
	}
	return m, nil
}
func (m browserFormModel) saveCmd() tea.Cmd {
	cfg := m.cfg
	cfgPath := m.cfgPath
	return func() tea.Msg {
		cfg.Browser.ProfileDir = strings.TrimSpace(m.fields[0].input.Value())
		cfg.Browser.Binary = strings.TrimSpace(m.fields[1].input.Value())
		if p, err := strconv.Atoi(strings.TrimSpace(m.fields[2].input.Value())); err == nil {
			cfg.Browser.CDPPort = p
		}
		cfg.Browser.Headless = m.fields[3].value
		if err := config.Save(cfgPath, cfg); err != nil {
			return errMsg(err.Error())
		}
		return savedMsg{}
	}
}
func (m browserFormModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("Browser Settings") + "\n")
	b.WriteString(tuiSectionStyle.Render(strings.Repeat("─", maxInt(24, minInt(72, widthOrDefault(m.width)-2)))))
	b.WriteString("\n")
	b.WriteString(renderSimpleFields(m.fields, m.cursor))
	if m.saved {
		b.WriteString("\n" + tuiSuccessStyle.Render("Saved."))
	}
	if m.err != "" {
		b.WriteString("\n" + tuiErrorStyle.Render(m.err))
	}
	b.WriteString("\n\n" + tuiHelpStyle.Render("Tab/↑/↓ navigate · Space toggle · Ctrl+S save · Esc/q quit"))
	return b.String()
}

type schedulerFormModel struct {
	input   textinput.Model
	cfg     *config.Config
	cfgPath string
	width   int
	saved   bool
	err     string
}

func newSchedulerFormModel(cfg *config.Config, cfgPath string) schedulerFormModel {
	value := ""
	if cfg.Scheduler.Concurrency != nil {
		value = fmt.Sprintf("%v", cfg.Scheduler.Concurrency)
	}
	in := newInput("auto", value)
	in.Focus()
	return schedulerFormModel{input: in, cfg: cfg, cfgPath: cfgPath}
}
func (m schedulerFormModel) Init() tea.Cmd { return textinput.Blink }
func (m schedulerFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.input.Width = clampInputWidth(m.width)
	case savedMsg:
		m.saved = true
	case errMsg:
		m.err = string(msg)
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return m, tea.Quit
		case "enter", "ctrl+s":
			return m, m.saveCmd()
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
func (m schedulerFormModel) saveCmd() tea.Cmd {
	cfg := m.cfg
	cfgPath := m.cfgPath
	value := strings.TrimSpace(m.input.Value())
	return func() tea.Msg {
		if value == "" || value == "auto" {
			cfg.Scheduler.Concurrency = nil
		} else if p, err := strconv.Atoi(value); err == nil {
			cfg.Scheduler.Concurrency = p
		} else {
			return errMsg("Enter a number or 'auto'")
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return errMsg(err.Error())
		}
		return savedMsg{}
	}
}
func (m schedulerFormModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("Scheduler Settings") + "\n")
	b.WriteString(tuiSectionStyle.Render(strings.Repeat("─", maxInt(24, minInt(72, widthOrDefault(m.width)-2)))))
	b.WriteString("\n")
	b.WriteString(tuiLabelStyle.Render("Concurrency") + "  " + tuiDimStyle.Render("Number of parallel tasks, or 'auto'") + "\n")
	b.WriteString(m.input.View())
	if m.saved {
		b.WriteString("\n" + tuiSuccessStyle.Render("Saved."))
	}
	if m.err != "" {
		b.WriteString("\n" + tuiErrorStyle.Render(m.err))
	}
	b.WriteString("\n\n" + tuiHelpStyle.Render("Enter save · Esc/q quit"))
	return b.String()
}

func runServerForm(cfg *config.Config, cfgPath string) error {
	_, err := tea.NewProgram(newServerFormModel(cfg, cfgPath), tea.WithAltScreen()).Run()
	return err
}

func runGeneralForm(cfg *config.Config, cfgPath string) error {
	_, err := tea.NewProgram(newGeneralFormModel(cfg, cfgPath), tea.WithAltScreen()).Run()
	return err
}

func runBrowserForm(cfg *config.Config, cfgPath string) error {
	_, err := tea.NewProgram(newBrowserFormModel(cfg, cfgPath), tea.WithAltScreen()).Run()
	return err
}

func runSchedulerForm(cfg *config.Config, cfgPath string) error {
	_, err := tea.NewProgram(newSchedulerFormModel(cfg, cfgPath), tea.WithAltScreen()).Run()
	return err
}
