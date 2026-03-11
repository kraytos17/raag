package tui

import "charm.land/lipgloss/v2"

var (
	TitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true).
			Padding(0, 1)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, 1)

	LabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("75")).
			Bold(true)

	ValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255"))

	ProgressBarStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("86"))

	ProgressEmptyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240"))

	ListItemStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))

	ListSelectedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("86")).
				Background(lipgloss.Color("236")).
				Bold(true)

	StatusOnlineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("82")).
				Bold(true)

	StatusOfflineStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).
				Bold(true)

	HelpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))

	BorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))

	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	HighlightStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86"))
)
