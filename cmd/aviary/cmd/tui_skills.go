package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/skills"
)

type skillMgrMode int

const (
	skillModeList skillMgrMode = iota
	skillModeEdit
)

type skillMgrModel struct {
	cfg        *config.Config
	cfgPath    string
	mode       skillMgrMode
	cursor     int
	editCursor int
	width      int
	installed  []skills.Definition
	binary     textinput.Model
	allowed    textinput.Model
	message    string
	err        string
}

func newSkillMgrModel(cfg *config.Config, cfgPath string) skillMgrModel {
	binary := newInput("gog", "")
	allowed := newInput("gmail, calendar, drive", "")
	return skillMgrModel{
		cfg:     cfg,
		cfgPath: cfgPath,
		binary:  binary,
		allowed: allowed,
	}
}

func (m *skillMgrModel) refreshInstalled() {
	list, err := skills.ListInstalled(m.cfg)
	if err != nil {
		m.err = err.Error()
		return
	}
	m.installed = list
	if m.cursor >= len(m.installed) && len(m.installed) > 0 {
		m.cursor = len(m.installed) - 1
	}
}

func (m skillMgrModel) Init() tea.Cmd {
	m.refreshInstalled()
	return nil
}

func (m skillMgrModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.binary.Width = clampInputWidth(m.width)
		m.allowed.Width = clampInputWidth(m.width)
	case tea.KeyMsg:
		switch m.mode {
		case skillModeList:
			return m.updateList(msg)
		case skillModeEdit:
			return m.updateEdit(msg)
		}
	}
	return m, nil
}

func (m skillMgrModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k", "shift+tab":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j", "tab":
		if m.cursor < len(m.installed)-1 {
			m.cursor++
		}
	case "enter", " ":
		if len(m.installed) == 0 {
			return m, nil
		}
		name := m.installed[m.cursor].Name
		var current config.SkillConfig
		if m.cfg.Skills != nil {
			current = m.cfg.Skills[name]
		}
		m.binary.SetValue(current.Binary)
		m.allowed.SetValue(strings.Join(current.AllowedCommands, ", "))
		m.binary.Blur()
		m.allowed.Blur()
		m.editCursor = 0
		m.mode = skillModeEdit
	}
	return m, nil
}

func (m skillMgrModel) updateEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = skillModeList
		m.binary.Blur()
		m.allowed.Blur()
		return m, nil
	case "up", "k", "shift+tab":
		if m.editCursor > 0 {
			m.editCursor--
		}
		return m.syncFocus()
	case "down", "j", "tab":
		if m.editCursor < 3 {
			m.editCursor++
		}
		return m.syncFocus()
	case "enter", " ":
		switch m.editCursor {
		case 0:
			return m.toggleEnabled()
		case 3:
			return m.saveCurrent()
		}
	}

	var cmd tea.Cmd
	switch m.editCursor {
	case 1:
		m.binary, cmd = m.binary.Update(msg)
		return m, cmd
	case 2:
		m.allowed, cmd = m.allowed.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m skillMgrModel) syncFocus() (tea.Model, tea.Cmd) {
	m.binary.Blur()
	m.allowed.Blur()
	switch m.editCursor {
	case 1:
		m.binary.Focus()
		return m, textinput.Blink
	case 2:
		m.allowed.Focus()
		return m, textinput.Blink
	default:
		return m, nil
	}
}

func (m skillMgrModel) currentSkillName() string {
	if m.cursor < 0 || m.cursor >= len(m.installed) {
		return ""
	}
	return m.installed[m.cursor].Name
}

func (m skillMgrModel) toggleEnabled() (tea.Model, tea.Cmd) {
	name := m.currentSkillName()
	if name == "" {
		return m, nil
	}
	if m.cfg.Skills == nil {
		m.cfg.Skills = map[string]config.SkillConfig{}
	}
	sk := m.cfg.Skills[name]
	sk.Enabled = !sk.Enabled
	m.cfg.Skills[name] = sk
	return m.saveCurrent()
}

func (m skillMgrModel) saveCurrent() (tea.Model, tea.Cmd) {
	name := m.currentSkillName()
	if name == "" {
		return m, nil
	}
	if m.cfg.Skills == nil {
		m.cfg.Skills = map[string]config.SkillConfig{}
	}
	sk := m.cfg.Skills[name]
	sk.Binary = strings.TrimSpace(m.binary.Value())
	sk.AllowedCommands = splitCSV(m.allowed.Value())
	m.cfg.Skills[name] = sk
	if err := config.Save(m.cfgPath, m.cfg); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.message = fmt.Sprintf("Saved %s.", name)
	m.err = ""
	m.refreshInstalled()
	return m, nil
}

func (m skillMgrModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("Skills"))
	b.WriteString("\n")
	b.WriteString(tuiDimStyle.Render("Enable installed skills and edit skill-specific runtime settings."))
	b.WriteString("\n\n")
	switch m.mode {
	case skillModeList:
		b.WriteString(m.viewList())
	case skillModeEdit:
		b.WriteString(m.viewEdit())
	}
	if m.err != "" {
		b.WriteString("\n\n" + tuiErrorStyle.Render(m.err))
	}
	if m.message != "" {
		b.WriteString("\n\n" + tuiSuccessStyle.Render(m.message))
	}
	help := "Enter edit/select · Tab/↑/↓ navigate · Esc/q quit"
	if m.mode == skillModeEdit {
		help = "Enter toggle/save · Tab/↑/↓ navigate · Esc back"
	}
	b.WriteString("\n\n" + tuiHelpStyle.Render(help))
	return b.String()
}

func (m skillMgrModel) viewList() string {
	if len(m.installed) == 0 {
		return tuiDimStyle.Render("No installed skills found.")
	}
	var b strings.Builder
	for i, sk := range m.installed {
		status := tuiDimStyle.Render("disabled")
		if sk.Enabled {
			status = tuiSuccessStyle.Render("enabled")
		}
		line := fmt.Sprintf("%s %-18s  %s  %s", tuiCursor(i == m.cursor), sk.Name, status, tuiDimStyle.Render(sk.Source))
		if i == m.cursor {
			line = tuiSelectedStyle.Render(line)
		}
		b.WriteString(line + "\n")
		if sk.Description != "" {
			b.WriteString("  " + tuiDimStyle.Render(sk.Description) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m skillMgrModel) viewEdit() string {
	name := m.currentSkillName()
	if name == "" {
		return tuiDimStyle.Render("No installed skills found.")
	}
	sk := m.cfg.Skills[name]
	rows := []string{
		fmt.Sprintf("Enabled          %s", skillBoolLabel(sk.Enabled)),
		fmt.Sprintf("Binary           %s", m.binary.View()),
		fmt.Sprintf("Allowed Commands %s", m.allowed.View()),
		"Save",
	}
	var b strings.Builder
	b.WriteString(tuiLabelStyle.Render("Edit Skill: " + name))
	b.WriteString("\n\n")
	for i, row := range rows {
		line := fmt.Sprintf("%s %s", tuiCursor(i == m.editCursor), row)
		if i == m.editCursor {
			line = tuiSelectedStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func skillBoolLabel(v bool) string {
	if v {
		return tuiSuccessStyle.Render("enabled")
	}
	return tuiDimStyle.Render("disabled")
}

func runSkillMgr(cfg *config.Config, cfgPath string) error {
	p := tea.NewProgram(newSkillMgrModel(cfg, cfgPath), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
}
