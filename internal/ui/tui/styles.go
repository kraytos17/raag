package tui

import "charm.land/lipgloss/v2"

var (
	surfaceColor = lipgloss.Color("#161b22")
	borderColor  = lipgloss.Color("#30363d")
	activeBorder = lipgloss.Color("#58a6ff")
	accentColor  = lipgloss.Color("#58a6ff")
	greenColor   = lipgloss.Color("#3fb950")
	orangeColor  = lipgloss.Color("#f78166")
	yellowColor  = lipgloss.Color("#e3b341")
	textColor    = lipgloss.Color("#e6edf3")
	subtleColor  = lipgloss.Color("#8b949e")
	dimColor     = lipgloss.Color("#484f58")
	errorColor   = lipgloss.Color("#f85149")
)

var (
	headerStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Background(surfaceColor)

	footerStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Background(surfaceColor)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderColor).
			Background(surfaceColor)

	activePanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(activeBorder).
				Background(surfaceColor)

	titleStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Bold(true)

	subtleStyle = lipgloss.NewStyle().
			Foreground(subtleColor)

	dimStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	statePlayingStyle = lipgloss.NewStyle().
				Foreground(greenColor)

	statePausedStyle = lipgloss.NewStyle().
				Foreground(yellowColor)

	stateBufferingStyle = lipgloss.NewStyle().
				Foreground(yellowColor)

	stateStoppedStyle = lipgloss.NewStyle().
				Foreground(dimColor)

	errorPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(errorColor).
			Background(surfaceColor).
			Padding(2, 4).
			Margin(2)

	helpPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(accentColor).
			Background(surfaceColor).
			Padding(1, 2).
			Margin(2)

	reconnectStyle = lipgloss.NewStyle().
			Foreground(orangeColor).
			Background(surfaceColor).
			Padding(1, 2).
			Margin(1)

	lyricsStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Background(surfaceColor).
			Padding(1, 2).
			Margin(1).
			MaxWidth(100)

	searchStyle = lipgloss.NewStyle().
			Foreground(textColor).
			Background(surfaceColor).
			Padding(0, 1).
			Margin(0, 2)
)
