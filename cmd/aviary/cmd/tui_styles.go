package cmd

import "github.com/charmbracelet/lipgloss"

var (
	tuiAccent = lipgloss.AdaptiveColor{Light: "#4f6bdc", Dark: "#92a8ff"}
	tuiDim    = lipgloss.AdaptiveColor{Light: "#666666", Dark: "#999999"}
	tuiGood   = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#4ac26b"}
	tuiBad    = lipgloss.AdaptiveColor{Light: "#c0392b", Dark: "#ff6b5f"}

	tuiTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(tuiAccent)
	tuiSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(tuiAccent)
	tuiDimStyle      = lipgloss.NewStyle().Foreground(tuiDim)
	tuiHelpStyle     = lipgloss.NewStyle().Foreground(tuiDim)
	tuiLabelStyle    = lipgloss.NewStyle().Bold(true)
	tuiSuccessStyle  = lipgloss.NewStyle().Bold(true).Foreground(tuiGood)
	tuiErrorStyle    = lipgloss.NewStyle().Foreground(tuiBad)
	tuiSectionStyle  = lipgloss.NewStyle().Foreground(tuiAccent)
)

func tuiCursor(selected bool) string {
	if selected {
		return tuiSelectedStyle.Render(">")
	}
	return " "
}
