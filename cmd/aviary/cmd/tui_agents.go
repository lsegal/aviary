package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

type agentMgrMode int

const (
	agentModeList agentMgrMode = iota
	agentModeEditMenu
	agentModeTextEdit
	agentModeChannels
	agentModeChannelEdit
	agentModeTasks
	agentModeTaskEdit
)

type agentTextTarget int

const (
	agentFieldName agentTextTarget = iota
	agentFieldModel
	agentFieldFallbacks
	agentFieldMemory
	agentFieldMemoryTokens
	agentFieldCompactKeep
	agentFieldRules
	agentFieldPermissionsPreset
	agentFieldPermissions
	agentFieldDisabledTools
	agentFieldFilesystemAllowedPaths
	agentFieldExecAllowedCommands
	agentFieldExecShell
	chFieldType
	chFieldToken
	chFieldID
	chFieldURL
	chFieldAllowFrom
	chFieldModel
	chFieldFallbacks
	taskFieldName
	taskFieldType
	taskFieldSchedule
	taskFieldStartAt
	taskFieldWatch
	taskFieldPrompt
	taskFieldScript
	taskFieldTarget
)

type agentMgrModel struct {
	cfg         *config.Config
	cfgPath     string
	mode        agentMgrMode
	cursor      int
	editCursor  int
	fieldCursor int
	textTarget  agentTextTarget
	editIdx     int
	channelIdx  int
	taskIdx     int
	isNew       bool
	draft       config.AgentConfig
	channel     config.ChannelConfig
	task        config.TaskConfig
	textInput   textinput.Model
	width       int
	message     string
	err         string
	pendingInit string
}

func newAgentMgrModel(cfg *config.Config, cfgPath string) agentMgrModel {
	return agentMgrModel{
		cfg:        cfg,
		cfgPath:    cfgPath,
		editIdx:    -1,
		channelIdx: -1,
		taskIdx:    -1,
		textInput:  newInput("", ""),
	}
}

func (m agentMgrModel) Init() tea.Cmd { return nil }

func (m agentMgrModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.textInput.Width = clampInputWidth(m.width)
		return m, nil
	case tea.KeyMsg:
		switch m.mode {
		case agentModeList:
			return m.updateList(msg)
		case agentModeEditMenu:
			return m.updateEditMenu(msg)
		case agentModeTextEdit:
			return m.updateTextEdit(msg)
		case agentModeChannels:
			return m.updateChannels(msg)
		case agentModeChannelEdit:
			return m.updateChannelEdit(msg)
		case agentModeTasks:
			return m.updateTasks(msg)
		case agentModeTaskEdit:
			return m.updateTaskEdit(msg)
		}
	}
	return m, nil
}

func (m agentMgrModel) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.cfg.Agents)-1 {
			m.cursor++
		}
	case "n":
		m.beginEdit(len(m.cfg.Agents), true)
	case "enter", " ":
		if len(m.cfg.Agents) > 0 {
			m.beginEdit(m.cursor, false)
		}
	case "d", "delete":
		if len(m.cfg.Agents) == 0 {
			return m, nil
		}
		name := m.cfg.Agents[m.cursor].Name
		m.cfg.Agents = append(m.cfg.Agents[:m.cursor], m.cfg.Agents[m.cursor+1:]...)
		if m.cursor > 0 && m.cursor >= len(m.cfg.Agents) {
			m.cursor--
		}
		m.mode = agentModeList
		return m, m.saveConfig("Deleted " + name)
	}
	return m, nil
}

func (m *agentMgrModel) beginEdit(index int, isNew bool) {
	m.isNew = isNew
	m.editIdx = index
	m.editCursor = 0
	m.err = ""
	m.message = ""
	if isNew {
		m.draft = config.AgentConfig{}
	} else {
		m.draft = m.cfg.Agents[index]
	}
	m.mode = agentModeEditMenu
}

func (m agentMgrModel) updateEditMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.editMenuItems()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = agentModeList
	case "up", "k", "shift+tab":
		if m.editCursor > 0 {
			m.editCursor--
		}
	case "down", "j", "tab":
		if m.editCursor < len(items)-1 {
			m.editCursor++
		}
	case "enter", " ":
		return m.activateEditMenuItem()
	}
	return m, nil
}

func (m agentMgrModel) activateEditMenuItem() (tea.Model, tea.Cmd) {
	switch m.editCursor {
	case 0:
		return m.openTextEditor(agentFieldName, m.draft.Name, "assistant")
	case 1:
		return m.openTextEditor(agentFieldModel, m.draft.Model, "provider/model")
	case 2:
		return m.openTextEditor(agentFieldFallbacks, strings.Join(m.draft.Fallbacks, ", "), "model-a, model-b")
	case 3:
		return m.openTextEditor(agentFieldMemory, m.draft.Memory, "memory pool")
	case 4:
		return m.openTextEditor(agentFieldMemoryTokens, intString(m.draft.MemoryTokens), "0")
	case 5:
		return m.openTextEditor(agentFieldCompactKeep, intString(m.draft.CompactKeep), "0")
	case 6:
		return m.openTextEditor(agentFieldRules, m.draft.Rules, "inline rules or path")
	case 7:
		current := ""
		if m.draft.Permissions != nil {
			current = string(config.EffectivePermissionsPreset(m.draft.Permissions))
		}
		return m.openTextEditor(agentFieldPermissionsPreset, current, "standard")
	case 8:
		current := ""
		if m.draft.Permissions != nil {
			current = strings.Join(m.draft.Permissions.Tools, ", ")
		}
		return m.openTextEditor(agentFieldPermissions, current, "tool_a, tool_b")
	case 9:
		current := ""
		if m.draft.Permissions != nil {
			current = strings.Join(m.draft.Permissions.DisabledTools, ", ")
		}
		return m.openTextEditor(agentFieldDisabledTools, current, "tool_a, tool_b")
	case 10:
		current := ""
		if m.draft.Permissions != nil && m.draft.Permissions.Filesystem != nil {
			current = strings.Join(m.draft.Permissions.Filesystem.AllowedPaths, ", ")
		}
		return m.openTextEditor(agentFieldFilesystemAllowedPaths, current, "path-a, !path-b")
	case 11:
		current := ""
		if m.draft.Permissions != nil && m.draft.Permissions.Exec != nil {
			current = strings.Join(m.draft.Permissions.Exec.AllowedCommands, ", ")
		}
		return m.openTextEditor(agentFieldExecAllowedCommands, current, "cmd-a, !cmd-b")
	case 12:
		perms := m.ensureDraftPermissions()
		if perms.Exec == nil {
			perms.Exec = &config.ExecPermissionsConfig{}
		}
		perms.Exec.ShellInterpolate = !perms.Exec.ShellInterpolate
		m.compactDraftPermissions()
	case 13:
		current := ""
		if m.draft.Permissions != nil && m.draft.Permissions.Exec != nil {
			current = m.draft.Permissions.Exec.Shell
		}
		return m.openTextEditor(agentFieldExecShell, current, "pwsh")
	case 14:
		m.mode = agentModeChannels
		if m.channelIdx < 0 {
			m.channelIdx = 0
		}
	case 15:
		m.mode = agentModeTasks
		if m.taskIdx < 0 {
			m.taskIdx = 0
		}
	case 16:
		return m.saveEditedAgent()
	case 17:
		m.mode = agentModeList
	}
	return m, nil
}

func (m agentMgrModel) openTextEditor(target agentTextTarget, value, placeholder string) (tea.Model, tea.Cmd) {
	m.textTarget = target
	m.textInput.SetValue(value)
	m.textInput.Placeholder = placeholder
	m.textInput.Focus()
	m.mode = agentModeTextEdit
	return m, textinput.Blink
}

func (m agentMgrModel) updateTextEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.textInput.Blur()
		m.mode = m.parentModeForTextTarget()
		return m, nil
	case "enter":
		return m.applyTextEdit()
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m agentMgrModel) parentModeForTextTarget() agentMgrMode {
	switch m.textTarget {
	case chFieldType, chFieldToken, chFieldID, chFieldURL, chFieldAllowFrom, chFieldModel, chFieldFallbacks:
		return agentModeChannelEdit
	case taskFieldName, taskFieldType, taskFieldSchedule, taskFieldStartAt, taskFieldWatch, taskFieldPrompt, taskFieldScript, taskFieldTarget:
		return agentModeTaskEdit
	default:
		return agentModeEditMenu
	}
}

func (m agentMgrModel) applyTextEdit() (tea.Model, tea.Cmd) {
	value := strings.TrimSpace(m.textInput.Value())
	switch m.textTarget {
	case agentFieldName:
		m.draft.Name = value
	case agentFieldModel:
		m.draft.Model = value
	case agentFieldFallbacks:
		m.draft.Fallbacks = splitCSV(value)
	case agentFieldMemory:
		m.draft.Memory = value
	case agentFieldMemoryTokens:
		m.draft.MemoryTokens = parseInt(value)
	case agentFieldCompactKeep:
		m.draft.CompactKeep = parseInt(value)
	case agentFieldRules:
		m.draft.Rules = value
	case agentFieldPermissionsPreset:
		switch config.PermissionsPreset(value) {
		case "", config.PermissionsPresetFull, config.PermissionsPresetStandard, config.PermissionsPresetMinimal:
			if strings.TrimSpace(value) == "" || config.PermissionsPreset(value) == config.PermissionsPresetStandard {
				if m.draft.Permissions != nil {
					m.draft.Permissions.Preset = ""
				}
			} else {
				m.ensureDraftPermissions().Preset = config.PermissionsPreset(value)
			}
			m.compactDraftPermissions()
		}
	case agentFieldPermissions:
		m.ensureDraftPermissions().Tools = splitCSV(value)
		m.compactDraftPermissions()
	case agentFieldDisabledTools:
		m.ensureDraftPermissions().DisabledTools = splitCSV(value)
		m.compactDraftPermissions()
	case agentFieldFilesystemAllowedPaths:
		allowedPaths := splitCSV(value)
		perms := m.ensureDraftPermissions()
		if len(allowedPaths) == 0 {
			perms.Filesystem = nil
		} else {
			perms.Filesystem = &config.FilesystemPermissionsConfig{AllowedPaths: allowedPaths}
		}
		m.compactDraftPermissions()
	case agentFieldExecAllowedCommands:
		allowedCommands := splitCSV(value)
		perms := m.ensureDraftPermissions()
		if len(allowedCommands) == 0 {
			if perms.Exec != nil {
				perms.Exec.AllowedCommands = nil
			}
		} else {
			if perms.Exec == nil {
				perms.Exec = &config.ExecPermissionsConfig{}
			}
			perms.Exec.AllowedCommands = allowedCommands
		}
		m.compactDraftPermissions()
	case agentFieldExecShell:
		perms := m.ensureDraftPermissions()
		if strings.TrimSpace(value) == "" {
			if perms.Exec != nil {
				perms.Exec.Shell = ""
			}
		} else {
			if perms.Exec == nil {
				perms.Exec = &config.ExecPermissionsConfig{}
			}
			perms.Exec.Shell = value
		}
		m.compactDraftPermissions()
	case chFieldType:
		m.channel.Type = value
	case chFieldToken:
		m.channel.Token = value
	case chFieldID:
		m.channel.ID = value
	case chFieldURL:
		m.channel.URL = value
	case chFieldAllowFrom:
		m.channel.AllowFrom = parseAllowFrom(value)
	case chFieldModel:
		m.channel.Model = value
	case chFieldFallbacks:
		m.channel.Fallbacks = splitCSV(value)
	case taskFieldName:
		m.task.Name = value
	case taskFieldType:
		if value == "script" {
			m.task.Type = "script"
		} else {
			m.task.Type = "prompt"
		}
	case taskFieldSchedule:
		m.task.Schedule = value
	case taskFieldStartAt:
		m.task.StartAt = value
	case taskFieldWatch:
		m.task.Watch = value
	case taskFieldPrompt:
		m.task.Prompt = value
	case taskFieldScript:
		m.task.Prompt = value
	case taskFieldTarget:
		m.task.Target = value
	}
	m.textInput.Blur()
	m.mode = m.parentModeForTextTarget()
	return m, nil
}

func (m agentMgrModel) saveEditedAgent() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.draft.Name)
	model := strings.TrimSpace(m.draft.Model)
	if name == "" {
		m.err = "Agent name cannot be empty"
		return m, nil
	}
	if model == "" {
		m.err = "Agent model cannot be empty"
		return m, nil
	}
	for i, agent := range m.cfg.Agents {
		if agent.Name == name && (m.isNew || i != m.editIdx) {
			m.err = fmt.Sprintf("Agent %q already exists", name)
			return m, nil
		}
	}
	if m.isNew {
		m.cfg.Agents = append(m.cfg.Agents, m.draft)
		m.cursor = len(m.cfg.Agents) - 1
		m.pendingInit = name
	} else {
		m.cfg.Agents[m.editIdx] = m.draft
		m.cursor = m.editIdx
		m.pendingInit = ""
	}
	m.mode = agentModeList
	return m, m.saveConfig("Saved " + name)
}

func (m agentMgrModel) updateChannels(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = agentModeEditMenu
	case "up", "k":
		if m.channelIdx > 0 {
			m.channelIdx--
		}
	case "down", "j":
		if m.channelIdx < len(m.draft.Channels)-1 {
			m.channelIdx++
		}
	case "n":
		m.channel = config.ChannelConfig{Type: "signal", AllowFrom: []config.AllowFromEntry{{From: "*"}}}
		m.fieldCursor = 0
		m.channelIdx = len(m.draft.Channels)
		m.mode = agentModeChannelEdit
	case "d", "delete":
		if len(m.draft.Channels) == 0 {
			return m, nil
		}
		m.draft.Channels = append(m.draft.Channels[:m.channelIdx], m.draft.Channels[m.channelIdx+1:]...)
		if m.channelIdx > 0 && m.channelIdx >= len(m.draft.Channels) {
			m.channelIdx--
		}
	case "enter", " ":
		if len(m.draft.Channels) == 0 {
			return m, nil
		}
		m.channel = m.draft.Channels[m.channelIdx]
		m.fieldCursor = 0
		m.mode = agentModeChannelEdit
	}
	return m, nil
}

func (m agentMgrModel) updateChannelEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = agentModeChannels
	case "up", "k", "shift+tab":
		if m.fieldCursor > 0 {
			m.fieldCursor--
		}
	case "down", "j", "tab":
		if m.fieldCursor < len(m.channelFieldLabels())-1 {
			m.fieldCursor++
		}
	case "enter", " ":
		return m.activateChannelField()
	}
	return m, nil
}

func (m agentMgrModel) activateChannelField() (tea.Model, tea.Cmd) {
	switch m.fieldCursor {
	case 0:
		return m.openTextEditor(chFieldType, m.channel.Type, "signal")
	case 1:
		return m.openTextEditor(chFieldToken, m.channel.Token, "auth ref or token")
	case 2:
		return m.openTextEditor(chFieldID, m.channel.ID, "workspace-bot or +15551234567")
	case 3:
		return m.openTextEditor(chFieldURL, m.channel.URL, "127.0.0.1:7583")
	case 4:
		return m.openTextEditor(chFieldAllowFrom, joinAllowFrom(m.channel.AllowFrom), "*, U123, +1555")
	case 5:
		return m.openTextEditor(chFieldModel, m.channel.Model, "provider/model")
	case 6:
		return m.openTextEditor(chFieldFallbacks, strings.Join(m.channel.Fallbacks, ", "), "model-a, model-b")
	case 7:
		m.channel.ShowTyping = toggleBoolPtr(m.channel.ShowTyping, true)
	case 8:
		m.channel.ReactToEmoji = toggleBoolPtr(m.channel.ReactToEmoji, true)
	case 9:
		m.channel.ReplyToReplies = toggleBoolPtr(m.channel.ReplyToReplies, true)
	case 10:
		m.channel.SendReadReceipts = toggleBoolPtr(m.channel.SendReadReceipts, true)
	case 11:
		if m.channelIdx >= 0 && m.channelIdx < len(m.draft.Channels) {
			m.draft.Channels[m.channelIdx] = m.channel
		} else {
			m.draft.Channels = append(m.draft.Channels, m.channel)
			m.channelIdx = len(m.draft.Channels) - 1
		}
		m.mode = agentModeChannels
	case 12:
		m.mode = agentModeChannels
	}
	return m, nil
}

func (m agentMgrModel) updateTasks(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = agentModeEditMenu
	case "up", "k":
		if m.taskIdx > 0 {
			m.taskIdx--
		}
	case "down", "j":
		if m.taskIdx < len(m.draft.Tasks)-1 {
			m.taskIdx++
		}
	case "n":
		m.task = config.TaskConfig{Type: "prompt"}
		m.fieldCursor = 0
		m.taskIdx = len(m.draft.Tasks)
		m.mode = agentModeTaskEdit
	case "d", "delete":
		if len(m.draft.Tasks) == 0 {
			return m, nil
		}
		m.draft.Tasks = append(m.draft.Tasks[:m.taskIdx], m.draft.Tasks[m.taskIdx+1:]...)
		if m.taskIdx > 0 && m.taskIdx >= len(m.draft.Tasks) {
			m.taskIdx--
		}
	case "enter", " ":
		if len(m.draft.Tasks) == 0 {
			return m, nil
		}
		m.task = m.draft.Tasks[m.taskIdx]
		m.fieldCursor = 0
		m.mode = agentModeTaskEdit
	}
	return m, nil
}

func (m agentMgrModel) updateTaskEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = agentModeTasks
	case "up", "k", "shift+tab":
		if m.fieldCursor > 0 {
			m.fieldCursor--
		}
	case "down", "j", "tab":
		if m.fieldCursor < len(m.taskFieldLabels())-1 {
			m.fieldCursor++
		}
	case "enter", " ":
		return m.activateTaskField()
	}
	return m, nil
}

func (m agentMgrModel) activateTaskField() (tea.Model, tea.Cmd) {
	switch m.fieldCursor {
	case 0:
		return m.openTextEditor(taskFieldName, m.task.Name, "daily-report")
	case 1:
		return m.openTextEditor(taskFieldType, taskTypeValue(m.task), "prompt or script")
	case 2:
		return m.openTextEditor(taskFieldSchedule, m.task.Schedule, "0 9 * * *")
	case 3:
		return m.openTextEditor(taskFieldStartAt, m.task.StartAt, "RFC3339")
	case 4:
		m.task.RunOnce = !m.task.RunOnce
	case 5:
		return m.openTextEditor(taskFieldWatch, m.task.Watch, "*.md")
	case 6:
		if taskTypeValue(m.task) == "script" {
			return m.openTextEditor(taskFieldScript, m.task.Prompt, "print('hello from lua')")
		}
		return m.openTextEditor(taskFieldPrompt, m.task.Prompt, "task prompt")
	case 7:
		return m.openTextEditor(taskFieldTarget, m.task.Target, "route:signal:+15551234567:+15557654321")
	case 8:
		if m.taskIdx >= 0 && m.taskIdx < len(m.draft.Tasks) {
			m.draft.Tasks[m.taskIdx] = m.task
		} else {
			m.draft.Tasks = append(m.draft.Tasks, m.task)
			m.taskIdx = len(m.draft.Tasks) - 1
		}
		m.mode = agentModeTasks
	case 9:
		m.mode = agentModeTasks
	}
	return m, nil
}

func (m agentMgrModel) saveConfig(_ string) tea.Cmd {
	cfg := m.cfg
	cfgPath := m.cfgPath
	pendingInit := m.pendingInit
	return func() tea.Msg {
		if err := config.Save(cfgPath, cfg); err != nil {
			return errMsg(err.Error())
		}
		if pendingInit != "" {
			if err := store.SyncAgentTemplate(pendingInit); err != nil {
				return errMsg(err.Error())
			}
		}
		return savedMsg{}
	}
}

func (m agentMgrModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("Agent Manager"))
	b.WriteString("\n")
	b.WriteString(tuiSectionStyle.Render(strings.Repeat("─", maxInt(24, minInt(72, widthOrDefault(m.width)-2)))))
	b.WriteString("\n")
	switch m.mode {
	case agentModeList:
		b.WriteString(m.viewAgentList())
	case agentModeEditMenu:
		b.WriteString(m.viewEditMenu())
	case agentModeTextEdit:
		b.WriteString(m.viewTextEditor())
	case agentModeChannels:
		b.WriteString(m.viewChannels())
	case agentModeChannelEdit:
		b.WriteString(m.viewChannelEditor())
	case agentModeTasks:
		b.WriteString(m.viewTasks())
	case agentModeTaskEdit:
		b.WriteString(m.viewTaskEditor())
	}
	if m.message != "" {
		b.WriteString("\n" + tuiSuccessStyle.Render(m.message))
	}
	if m.err != "" {
		b.WriteString("\n" + tuiErrorStyle.Render(m.err))
	}
	return b.String()
}

func (m agentMgrModel) viewAgentList() string {
	var b strings.Builder
	if len(m.cfg.Agents) == 0 {
		b.WriteString(tuiDimStyle.Render("No agents configured.") + "\n")
	} else {
		for i, agent := range m.cfg.Agents {
			name := fmtPadRight(agent.Name, 18)
			model := agent.Model
			if i == m.cursor {
				name = tuiSelectedStyle.Render(name)
				model = tuiSelectedStyle.Render(model)
			}
			b.WriteString(tuiCursor(i == m.cursor) + " " + name + "  " + model + "\n")
		}
	}
	b.WriteString("\n" + tuiHelpStyle.Render("Enter edit · n new · d delete · ↑/↓ navigate · Esc/q quit"))
	return b.String()
}

func (m agentMgrModel) viewEditMenu() string {
	var b strings.Builder
	b.WriteString(tuiLabelStyle.Render("Editing "+fallback(m.draft.Name, "<new agent>")) + "\n\n")
	for i, item := range m.editMenuItems() {
		label := fmtPadRight(item[0], 18)
		value := item[1]
		if i == m.editCursor {
			label = tuiSelectedStyle.Render(label)
			value = tuiSelectedStyle.Render(value)
		}
		b.WriteString(tuiCursor(i == m.editCursor) + " " + label + "  " + value + "\n")
	}
	b.WriteString("\n" + tuiHelpStyle.Render("Enter select · ↑/↓ navigate · Esc back"))
	return b.String()
}

func (m agentMgrModel) viewTextEditor() string {
	return m.textInput.View() + "\n\n" + tuiHelpStyle.Render("Enter save · Esc back")
}

func (m agentMgrModel) viewChannels() string {
	var b strings.Builder
	b.WriteString(tuiLabelStyle.Render("Channels") + "\n\n")
	if len(m.draft.Channels) == 0 {
		b.WriteString(tuiDimStyle.Render("No channels configured.") + "\n")
	} else {
		for i, ch := range m.draft.Channels {
			label := fmtPadRight(ch.Type, 12)
			desc := firstNonEmpty(ch.ID, ch.URL, "(no target)")
			if i == m.channelIdx {
				label = tuiSelectedStyle.Render(label)
				desc = tuiSelectedStyle.Render(desc)
			}
			b.WriteString(tuiCursor(i == m.channelIdx) + " " + label + "  " + desc + "\n")
		}
	}
	b.WriteString("\n" + tuiHelpStyle.Render("Enter edit · n new · d delete · Esc back"))
	return b.String()
}

func (m agentMgrModel) viewChannelEditor() string {
	values := []string{
		fallback(m.channel.Type, "signal"),
		m.channel.Token,
		m.channel.ID,
		m.channel.URL,
		joinAllowFrom(m.channel.AllowFrom),
		m.channel.Model,
		strings.Join(m.channel.Fallbacks, ", "),
		boolLabel(config.BoolOr(m.channel.ShowTyping, true)),
		boolLabel(config.BoolOr(m.channel.ReactToEmoji, true)),
		boolLabel(config.BoolOr(m.channel.ReplyToReplies, true)),
		boolLabel(config.BoolOr(m.channel.SendReadReceipts, true)),
		"save",
		"back",
	}
	labels := m.channelFieldLabels()
	var b strings.Builder
	b.WriteString(tuiLabelStyle.Render("Channel") + "\n\n")
	for i := range labels {
		label := fmtPadRight(labels[i], 18)
		value := values[i]
		if i == m.fieldCursor {
			label = tuiSelectedStyle.Render(label)
			value = tuiSelectedStyle.Render(value)
		}
		b.WriteString(tuiCursor(i == m.fieldCursor) + " " + label + "  " + value + "\n")
	}
	b.WriteString("\n" + tuiHelpStyle.Render("Enter edit/toggle · ↑/↓ navigate · Esc back"))
	return b.String()
}

func (m agentMgrModel) viewTasks() string {
	var b strings.Builder
	b.WriteString(tuiLabelStyle.Render("Tasks") + "\n\n")
	if len(m.draft.Tasks) == 0 {
		b.WriteString(tuiDimStyle.Render("No tasks configured.") + "\n")
	} else {
		for i, task := range m.draft.Tasks {
			label := fmtPadRight(task.Name, 18)
			desc := firstNonEmpty(task.Schedule, task.Watch, taskContentValue(task), "(empty)")
			if i == m.taskIdx {
				label = tuiSelectedStyle.Render(label)
				desc = tuiSelectedStyle.Render(desc)
			}
			b.WriteString(tuiCursor(i == m.taskIdx) + " " + label + "  " + desc + "\n")
		}
	}
	b.WriteString("\n" + tuiHelpStyle.Render("Enter edit · n new · d delete · Esc back"))
	return b.String()
}

func (m agentMgrModel) viewTaskEditor() string {
	values := []string{
		m.task.Name,
		taskTypeValue(m.task),
		m.task.Schedule,
		m.task.StartAt,
		boolLabel(m.task.RunOnce),
		m.task.Watch,
		taskContentValue(m.task),
		m.task.Target,
		"save",
		"back",
	}
	labels := m.taskFieldLabels()
	var b strings.Builder
	b.WriteString(tuiLabelStyle.Render("Task") + "\n\n")
	for i := range labels {
		label := fmtPadRight(labels[i], 18)
		value := values[i]
		if i == m.fieldCursor {
			label = tuiSelectedStyle.Render(label)
			value = tuiSelectedStyle.Render(value)
		}
		b.WriteString(tuiCursor(i == m.fieldCursor) + " " + label + "  " + value + "\n")
	}
	b.WriteString("\n" + tuiHelpStyle.Render("Enter edit/toggle · ↑/↓ navigate · Esc back"))
	return b.String()
}

func (m agentMgrModel) editMenuItems() [][2]string {
	preset := string(config.PermissionsPresetStandard)
	perms := ""
	disabledTools := ""
	filesystemAllowedPaths := ""
	execAllowedCommands := ""
	execShellInterpolate := boolLabel(false)
	execShell := ""
	if m.draft.Permissions != nil {
		preset = string(config.EffectivePermissionsPreset(m.draft.Permissions))
		perms = strings.Join(m.draft.Permissions.Tools, ", ")
		disabledTools = strings.Join(m.draft.Permissions.DisabledTools, ", ")
		if m.draft.Permissions.Filesystem != nil {
			filesystemAllowedPaths = strings.Join(m.draft.Permissions.Filesystem.AllowedPaths, ", ")
		}
		if m.draft.Permissions.Exec != nil {
			execAllowedCommands = strings.Join(m.draft.Permissions.Exec.AllowedCommands, ", ")
			execShellInterpolate = boolLabel(m.draft.Permissions.Exec.ShellInterpolate)
			execShell = m.draft.Permissions.Exec.Shell
		}
	}
	return [][2]string{
		{"Name", m.draft.Name},
		{"Model", m.draft.Model},
		{"Fallbacks", strings.Join(m.draft.Fallbacks, ", ")},
		{"Memory", m.draft.Memory},
		{"Memory tokens", intString(m.draft.MemoryTokens)},
		{"Compact keep", intString(m.draft.CompactKeep)},
		{"Rules", m.draft.Rules},
		{"Permissions preset", preset},
		{"Allowed tools", perms},
		{"Disabled tools", disabledTools},
		{"Filesystem paths", filesystemAllowedPaths},
		{"Exec commands", execAllowedCommands},
		{"Exec interpolate", execShellInterpolate},
		{"Exec shell", execShell},
		{"Channels", fmt.Sprintf("%d configured", len(m.draft.Channels))},
		{"Tasks", fmt.Sprintf("%d configured", len(m.draft.Tasks))},
		{"Save", "write config"},
		{"Back", "discard editor"},
	}
}

func (m *agentMgrModel) ensureDraftPermissions() *config.PermissionsConfig {
	if m.draft.Permissions == nil {
		m.draft.Permissions = &config.PermissionsConfig{}
	}
	return m.draft.Permissions
}

func (m *agentMgrModel) compactDraftPermissions() {
	if m.draft.Permissions == nil {
		return
	}
	perms := m.draft.Permissions
	if len(perms.Tools) == 0 {
		perms.Tools = nil
	}
	if len(perms.DisabledTools) == 0 {
		perms.DisabledTools = nil
	}
	if perms.Filesystem != nil && len(perms.Filesystem.AllowedPaths) == 0 {
		perms.Filesystem = nil
	}
	if perms.Exec != nil {
		if len(perms.Exec.AllowedCommands) == 0 {
			perms.Exec.AllowedCommands = nil
		}
		if len(perms.Exec.AllowedCommands) == 0 && !perms.Exec.ShellInterpolate && strings.TrimSpace(perms.Exec.Shell) == "" {
			perms.Exec = nil
		}
	}
	if config.EffectivePermissionsPreset(perms) == config.PermissionsPresetStandard {
		perms.Preset = ""
	}
	if len(perms.Tools) == 0 &&
		len(perms.DisabledTools) == 0 &&
		perms.Filesystem == nil &&
		perms.Exec == nil &&
		perms.Preset == "" {
		m.draft.Permissions = nil
	}
}

func (m agentMgrModel) channelFieldLabels() []string {
	return []string{
		"Type", "Token", "ID", "URL", "AllowFrom", "Model", "Fallbacks",
		"Show typing", "React emoji", "Reply replies", "Read receipts", "Save", "Back",
	}
}

func (m agentMgrModel) taskFieldLabels() []string {
	contentLabel := "Prompt"
	if taskTypeValue(m.task) == "script" {
		contentLabel = "Script"
	}
	return []string{"Name", "Type", "Schedule", "Start at", "Run once", "Watch", contentLabel, "Target", "Save", "Back"}
}

func taskTypeValue(task config.TaskConfig) string {
	if strings.TrimSpace(task.Type) == "script" {
		return "script"
	}
	return "prompt"
}

func taskContentValue(task config.TaskConfig) string {
	if taskTypeValue(task) == "script" {
		return task.Prompt
	}
	return task.Prompt
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func parseInt(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}

func toggleBoolPtr(v *bool, def bool) *bool {
	next := !config.BoolOr(v, def)
	return &next
}

func joinAllowFrom(entries []config.AllowFromEntry) string {
	if len(entries) == 0 {
		return ""
	}
	values := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.TrimSpace(entry.From) != "" {
			values = append(values, strings.TrimSpace(entry.From))
		}
	}
	return strings.Join(values, ", ")
}

func parseAllowFrom(value string) []config.AllowFromEntry {
	parts := splitCSV(value)
	if len(parts) == 0 {
		return nil
	}
	out := make([]config.AllowFromEntry, 0, len(parts))
	for _, part := range parts {
		out = append(out, config.AllowFromEntry{From: part})
	}
	return out
}

func intString(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v)
}

func boolLabel(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func fallback(v, empty string) string {
	if strings.TrimSpace(v) == "" {
		return empty
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func runAgentMgr(cfg *config.Config, cfgPath string) error {
	p := tea.NewProgram(newAgentMgrModel(cfg, cfgPath), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := final.(agentMgrModel); ok && fm.err != "" {
		return fmt.Errorf("%s", fm.err)
	}
	return nil
}
