package cmd

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	authpkg "github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
)

type configureMenuSelection int

const (
	configureMenuGeneral configureMenuSelection = iota
	configureMenuProviders
	configureMenuAgents
	configureMenuSkills
	configureMenuServer
	configureMenuBrowser
	configureMenuScheduler
	configureMenuQuit
)

type configureMenuItem struct {
	selection configureMenuSelection
	title     string
	summary   string
	detail    string
}

type configureMenuModel struct {
	cursor    int
	items     []configureMenuItem
	width     int
	height    int
	selection configureMenuSelection
}

func newConfigureMenuModel(cfg *config.Config, st authpkg.Store) configureMenuModel {
	return configureMenuModel{
		items: []configureMenuItem{
			{configureMenuGeneral, "General", configureGeneralSummary(cfg), "Shared runtime settings including server, browser, scheduler, and web search."},
			{configureMenuProviders, "Providers & Auth", configureProvidersSummary(st), "Manage provider credentials and OAuth logins."},
			{configureMenuAgents, "Agents", configureAgentsSummary(cfg), "Add, edit, or remove configured agents."},
			{configureMenuSkills, "Skills", configureSkillsSummary(cfg), "Enable installed skills and configure skill runtime settings."},
			{configureMenuServer, "Server", configureServerSummary(cfg), "Port, TLS, and external access."},
			{configureMenuBrowser, "Browser", configureBrowserSummary(cfg), "Browser binary, CDP port, profile, headless mode, and tab reuse."},
			{configureMenuScheduler, "Scheduler", configureSchedulerSummary(cfg), "Background task concurrency."},
			{configureMenuQuit, "Done", "Exit configure", "Return to the shell."},
		},
		selection: configureMenuQuit,
	}
}

func (m configureMenuModel) Init() tea.Cmd { return nil }

func (m configureMenuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.selection = configureMenuQuit
			return m, tea.Quit
		case "up", "k", "shift+tab":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j", "tab":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter", " ":
			m.selection = m.items[m.cursor].selection
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m configureMenuModel) View() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render("Configure Aviary"))
	b.WriteString("\n")
	b.WriteString(tuiDimStyle.Render("Compact config editor for providers, agents, and runtime settings."))
	b.WriteString("\n\n")
	b.WriteString(tuiSectionStyle.Render(strings.Repeat("─", maxInt(24, minInt(72, widthOrDefault(m.width)-2)))))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s  %s  %s\n\n",
		tuiLabelStyle.Render("Sections"),
		tuiDimStyle.Render(fmt.Sprintf("%d available", len(m.items))),
		tuiDimStyle.Render(configureProvidersSummaryFromItems(m.items)),
	)
	for i, item := range m.items {
		title := fmt.Sprintf("%-18s", item.title)
		summary := item.summary
		if i == m.cursor {
			title = tuiSelectedStyle.Render(title)
			summary = tuiSelectedStyle.Render(summary)
		}
		fmt.Fprintf(&b, "%s %s  %s\n", tuiCursor(i == m.cursor), title, summary)
	}
	if m.cursor >= 0 && m.cursor < len(m.items) {
		b.WriteString("\n")
		b.WriteString(tuiSectionStyle.Render(strings.Repeat("─", maxInt(24, minInt(72, widthOrDefault(m.width)-2)))))
		b.WriteString("\n")
		b.WriteString(tuiDimStyle.Render(m.items[m.cursor].detail))
	}
	b.WriteString("\n\n")
	b.WriteString(tuiHelpStyle.Render("Enter select · Tab/↑/↓ navigate · Esc/q quit"))
	return b.String()
}

func runConfigureMenu(cfg *config.Config, st authpkg.Store) (configureMenuSelection, error) {
	p := tea.NewProgram(newConfigureMenuModel(cfg, st), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return configureMenuQuit, err
	}
	if fm, ok := final.(configureMenuModel); ok {
		return fm.selection, nil
	}
	return configureMenuQuit, nil
}

func runWizard(cfg *config.Config, cfgPath string, st authpkg.Store) error {
	for {
		selection, err := runConfigureMenu(cfg, st)
		if err != nil {
			return err
		}
		switch selection {
		case configureMenuGeneral:
			if err := runGeneralForm(cfg, cfgPath); err != nil {
				return err
			}
		case configureMenuProviders:
			if err := runProviderMgr(st); err != nil {
				return err
			}
		case configureMenuAgents:
			if err := runAgentMgr(cfg, cfgPath); err != nil {
				return err
			}
		case configureMenuSkills:
			if err := runSkillMgr(cfg, cfgPath); err != nil {
				return err
			}
		case configureMenuServer:
			if err := runServerForm(cfg, cfgPath); err != nil {
				return err
			}
		case configureMenuBrowser:
			if err := runBrowserForm(cfg, cfgPath); err != nil {
				return err
			}
		case configureMenuScheduler:
			if err := runSchedulerForm(cfg, cfgPath); err != nil {
				return err
			}
		case configureMenuQuit:
			return nil
		}
	}
}

func configureProvidersSummary(st authpkg.Store) string {
	connected := 0
	for _, key := range []string{"anthropic:oauth", "anthropic:default", "openai:oauth", "openai:default", "gemini:oauth", "gemini:default"} {
		if _, err := st.Get(key); err == nil {
			connected++
		}
	}
	cfg, err := config.Load(cfgFile)
	if err == nil && strings.TrimSpace(cfg.Models.Providers["vllm"].BaseURI) != "" {
		connected++
	}
	total := 4
	if connected > total {
		connected = total
	}
	return fmt.Sprintf("%d/%d connected", connected, total)
}

func configureProvidersSummaryFromItems(items []configureMenuItem) string {
	for _, item := range items {
		if item.selection == configureMenuProviders {
			return item.summary
		}
	}
	return ""
}

func configureAgentsSummary(cfg *config.Config) string {
	if len(cfg.Agents) == 0 {
		return "No agents configured"
	}
	if len(cfg.Agents) == 1 {
		return "1 agent configured"
	}
	return fmt.Sprintf("%d agents configured", len(cfg.Agents))
}

func configureGeneralSummary(cfg *config.Config) string {
	parts := []string{
		configureServerSummary(cfg),
		configureBrowserSummary(cfg),
		configureSchedulerSummary(cfg),
	}
	if ref := strings.TrimSpace(cfg.Search.Web.BraveAPIKey); ref != "" {
		parts = append(parts, "Brave "+ref)
	} else {
		parts = append(parts, "Browser fallback only")
	}
	return strings.Join(parts, " · ")
}

func configureServerSummary(cfg *config.Config) string {
	host := "localhost"
	if cfg.Server.ExternalAccess {
		host = "external"
	}
	scheme := "https"
	if cfg.Server.NoTLS {
		scheme = "http"
	}
	return fmt.Sprintf("%s on %s:%d", scheme, host, cfg.Server.Port)
}

func configureSkillsSummary(cfg *config.Config) string {
	enabled := 0
	for _, sk := range cfg.Skills {
		if sk.Enabled {
			enabled++
		}
	}
	if enabled == 0 {
		return "No skills enabled"
	}
	if enabled == 1 {
		return "1 skill enabled"
	}
	return fmt.Sprintf("%d skills enabled", enabled)
}

func configureBrowserSummary(cfg *config.Config) string {
	mode := "headed"
	if cfg.Browser.Headless {
		mode = "headless"
	}
	reuse := "reuse tabs"
	if !config.EffectiveBrowserReuseTabs(cfg.Browser) {
		reuse = "new tabs only"
	}
	if strings.TrimSpace(cfg.Browser.Binary) != "" {
		return mode + ", " + reuse + ", custom binary"
	}
	return mode + ", " + reuse
}

func configureSchedulerSummary(cfg *config.Config) string {
	if cfg.Scheduler.Concurrency == nil || cfg.Scheduler.Concurrency == "auto" {
		return "Concurrency auto"
	}
	return fmt.Sprintf("Concurrency %v", cfg.Scheduler.Concurrency)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func widthOrDefault(v int) int {
	if v <= 0 {
		return 80
	}
	return v
}
