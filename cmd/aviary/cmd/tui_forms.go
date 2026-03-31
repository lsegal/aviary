package cmd

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lsegal/aviary/internal/config"
)

type savedMsg struct{}
type errMsg string

// simpleFormField holds display state for one form field.
type simpleFormField struct {
	label  string
	desc   string
	input  textinput.Model
	isBool bool
	value  bool
	path   string // dot-path used for save/load
}

// sectionFormModel is a generic TUI form driven by config.SectionFields.
// Creating a new form for any config section requires only a section key and
// title — no per-field boilerplate needed.
type sectionFormModel struct {
	title   string
	fields  []simpleFormField
	cursor  int
	cfg     *config.Config
	cfgPath string
	width   int
	saved   bool
	err     string
}

func newSectionFormModel(title, section string, cfg *config.Config, cfgPath string) sectionFormModel {
	metas := config.SectionFields(section)
	fields := make([]simpleFormField, 0, len(metas))
	for _, m := range metas {
		current := config.GetField(cfg, m.Path)
		if current == "" && m.Default != "" {
			current = m.Default
		}
		f := simpleFormField{label: m.Label, desc: m.Description, path: m.Path}
		switch m.Type {
		case "bool":
			f.isBool = true
			f.value = current == "true"
		default:
			f.input = newInput(m.Placeholder, current)
		}
		fields = append(fields, f)
	}
	if len(fields) > 0 && !fields[0].isBool {
		fields[0].input.Focus()
	}
	return sectionFormModel{
		title:   title,
		fields:  fields,
		cfg:     cfg,
		cfgPath: cfgPath,
	}
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
	w := totalWidth - 12
	if w < 16 {
		return 16
	}
	if w > 72 {
		return 72
	}
	return w
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

func (m sectionFormModel) Init() tea.Cmd { return textinput.Blink }

func (m sectionFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		for i := range m.fields {
			if !m.fields[i].isBool {
				m.fields[i].input.Width = clampInputWidth(m.width)
			}
		}
		return m, nil
	case savedMsg:
		m.saved = true
		return m, nil
	case errMsg:
		m.err = string(msg)
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	}
	if m.cursor < len(m.fields) && !m.fields[m.cursor].isBool {
		var cmd tea.Cmd
		m.fields[m.cursor].input, cmd = m.fields[m.cursor].input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m sectionFormModel) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case " ":
		if m.fields[m.cursor].isBool {
			m.fields[m.cursor].value = !m.fields[m.cursor].value
		}
	case "enter":
		if !m.fields[m.cursor].isBool && m.cursor == len(m.fields)-1 {
			return m, m.saveCmd()
		}
	case "ctrl+s":
		return m, m.saveCmd()
	}
	if m.cursor < len(m.fields) && !m.fields[m.cursor].isBool {
		var cmd tea.Cmd
		m.fields[m.cursor].input, cmd = m.fields[m.cursor].input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m sectionFormModel) saveCmd() tea.Cmd {
	cfg := m.cfg
	cfgPath := m.cfgPath
	// Snapshot values before entering the goroutine.
	type kv struct{ path, value string }
	vals := make([]kv, len(m.fields))
	for i, f := range m.fields {
		v := f.input.Value()
		if f.isBool {
			if f.value {
				v = "true"
			} else {
				v = "false"
			}
		}
		vals[i] = kv{f.path, v}
	}
	return func() tea.Msg {
		for _, kv := range vals {
			if err := config.SetField(cfg, kv.path, kv.value); err != nil {
				return errMsg(err.Error())
			}
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return errMsg(err.Error())
		}
		return savedMsg{}
	}
}

func (m sectionFormModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render(m.title) + "\n")
	b.WriteString(tuiSectionStyle.Render(strings.Repeat("─", maxInt(24, minInt(72, widthOrDefault(m.width)-2)))))
	b.WriteString("\n")
	if len(m.fields) == 0 {
		b.WriteString(tuiDimStyle.Render("No configurable fields for this section.") + "\n")
	} else {
		b.WriteString(renderSimpleFields(m.fields, m.cursor))
	}
	if m.saved {
		b.WriteString("\n" + tuiSuccessStyle.Render("Saved."))
	}
	if m.err != "" {
		b.WriteString("\n" + tuiErrorStyle.Render(m.err))
	}
	b.WriteString("\n\n" + tuiHelpStyle.Render("Tab/↑/↓ navigate · Space toggle · Ctrl+S save · Esc/q quit"))
	return b.String()
}

func runServerForm(cfg *config.Config, cfgPath string) error {
	_, err := tea.NewProgram(newSectionFormModel("Server Settings", "server", cfg, cfgPath), tea.WithAltScreen()).Run()
	return err
}

func runGeneralForm(cfg *config.Config, cfgPath string) error {
	_, err := tea.NewProgram(newSectionFormModel("General Settings", "general", cfg, cfgPath), tea.WithAltScreen()).Run()
	return err
}

func runBrowserForm(cfg *config.Config, cfgPath string) error {
	_, err := tea.NewProgram(newSectionFormModel("Browser Settings", "browser", cfg, cfgPath), tea.WithAltScreen()).Run()
	return err
}

func runSchedulerForm(cfg *config.Config, cfgPath string) error {
	_, err := tea.NewProgram(newSectionFormModel("Scheduler Settings", "scheduler", cfg, cfgPath), tea.WithAltScreen()).Run()
	return err
}
