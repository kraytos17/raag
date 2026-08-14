package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

func (m *Model) renderHeader() string {
	logo := titleStyle.Render("◈ RAAG")
	separator := dimStyle.Render(" │ ")

	var stateIndicator string
	switch m.Player.State {
	case StatePlaying:
		stateIndicator = statePlayingStyle.Render("▶ playing")
	case StatePaused:
		stateIndicator = statePausedStyle.Render("⏸ paused")
	case StateBuffering:
		stateIndicator = stateBufferingStyle.Render("⟳ buffering")
	default:
		stateIndicator = stateStoppedStyle.Render("■ stopped")
	}

	if m.Reconnecting {
		stateIndicator = lipgloss.NewStyle().Foreground(orangeColor).Render("⟳ reconnecting...")
	}

	peers := subtleStyle.Render(fmt.Sprintf("peers: %d", m.PeerCount))
	volume := subtleStyle.Render(fmt.Sprintf("vol: %d%%", m.Volume))
	version := dimStyle.Render("raag v2")

	right := lipgloss.JoinHorizontal(
		lipgloss.Top,
		peers,
		separator,
		volume,
	)

	fixedWidth := lipgloss.Width(logo) +
		lipgloss.Width(separator) +
		lipgloss.Width(stateIndicator) +
		lipgloss.Width(separator) +
		lipgloss.Width(right) +
		lipgloss.Width(separator) +
		lipgloss.Width(version)
	spacer := max(m.Width-fixedWidth, 1)

	header := lipgloss.JoinHorizontal(
		lipgloss.Top,
		logo,
		separator,
		stateIndicator,
		separator,
		right,
		lipgloss.NewStyle().Width(spacer).Render(""),
		version,
	)

	borderLine := dimStyle.Render(strings.Repeat("─", m.Width))

	return headerStyle.Render(header) + "\n" + borderLine
}

func (m *Model) renderWide() string {
	active := m.ActivePanel

	m.Library.Title = " Library "
	m.Queue.Title = " Queue "
	m.Peers.Title = " Peers "

	var library, queue, peers string
	library = activePanelStyle.Render(m.Library.View())
	queue = panelStyle.Render(m.Queue.View())
	peers = panelStyle.Render(m.Peers.View())

	switch active {
	case PanelLibrary:
		library = activePanelStyle.Render(m.Library.View())
	case PanelQueue:
		queue = activePanelStyle.Render(m.Queue.View())
	case PanelPeers:
		peers = activePanelStyle.Render(m.Peers.View())
	}

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		library,
		queue,
		peers,
	)
}

func (m *Model) renderNarrow() string {
	switch m.ActivePanel {
	case PanelLibrary:
		m.Library.Title = " ▶ Library "
		return activePanelStyle.Render(m.Library.View())
	case PanelQueue:
		m.Queue.Title = " ▶ Queue "
		return activePanelStyle.Render(m.Queue.View())
	case PanelPeers:
		m.Peers.Title = " ▶ Peers "
		return activePanelStyle.Render(m.Peers.View())
	}
	return ""
}

func (m *Model) renderFooter() string {
	var trackInfo string
	if m.Player.CurrentTrack != nil {
		trackInfo = fmt.Sprintf("%s — %s — %s",
			m.Player.CurrentTrack.Title,
			m.Player.CurrentTrack.Artist,
			m.Player.CurrentTrack.Album)
	} else {
		trackInfo = "No track playing"
	}

	posStr := formatTimeMs(m.Player.PositionMs)
	durStr := formatTimeMs(m.Player.DurationMs)

	seekWidth := max(m.Width-46, 10)
	seekBar := m.renderSeekBar(seekWidth)

	volumeWidth := 10
	volumeBar := m.renderVolumeBar(volumeWidth)

	controls := subtleStyle.Render(
		"[space] play/pause  [n] next  [p] prev  [f] +10s  [b] -10s  [?] help  [q] quit")

	row1 := lipgloss.NewStyle().Foreground(textColor).Render(trackInfo)
	row2 := seekBar + "  " + subtleStyle.Render(posStr+" / "+durStr)
	row3 := subtleStyle.Render("vol ") + volumeBar + subtleStyle.Render(fmt.Sprintf(" %d%%  ", m.Volume)) + controls

	borderLine := dimStyle.Render(strings.Repeat("─", m.Width))

	return footerStyle.Render(borderLine + "\n" + row1 + "\n" + row2 + "\n" + row3)
}

func (m *Model) renderSeekBar(width int) string {
	if m.Player.DurationMs <= 0 {
		return dimStyle.Render(strings.Repeat("─", width))
	}

	percent := float64(m.Player.PositionMs) / float64(m.Player.DurationMs)
	if percent > 1.0 {
		percent = 1.0
	}
	filled := min(int(float64(width-1)*percent), width-1)
	rest := width - 1 - filled

	var result strings.Builder
	result.WriteString(strings.Repeat("━", filled))
	result.WriteString("●")
	result.WriteString(strings.Repeat("─", rest))

	return lipgloss.NewStyle().Foreground(accentColor).Render(result.String())
}

func (m *Model) renderVolumeBar(width int) string {
	filled := int(float64(width) * float64(m.Volume) / 100)
	rest := width - filled

	var result strings.Builder
	result.WriteString(strings.Repeat("█", filled))
	result.WriteString(strings.Repeat("░", rest))

	return lipgloss.NewStyle().Foreground(accentColor).Render(result.String())
}

func (m *Model) renderHelpOverlay() string {
	helpContent := `Keybindings

  [Space]  Play/Pause
  [n]      Next track
  [p]      Previous track
  [x]      Stop
  [f]      Seek +10s
  [b]      Seek -10s
  [+]/[-]  Volume up/down
  [/]      Search
  [Tab]    Switch panel
  [Enter]  Play selected
  [d]      Remove from queue
  [r]      Refresh
  [?]      Toggle help
  [q]      Quit

Press Esc or ? to close`

	m.HelpViewport.SetContent(helpContent)

	content := helpPanelStyle.Render(m.HelpViewport.View())

	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		content,
	)
}

func (m *Model) renderErrorScreen() string {
	content := errorPanelStyle.Render(
		lipgloss.NewStyle().Foreground(errorColor).Bold(true).Render("Connection Error") + "\n\n" +
			lipgloss.NewStyle().Foreground(textColor).Render("Could not connect to daemon.\n\n") +
			subtleStyle.Render("Make sure raagd is running:\n") +
			statePlayingStyle.Render("  raagd\n\n") +
			dimStyle.Render("Socket: "+m.SocketPath),
	)

	return lipgloss.Place(
		m.Width,
		m.Height,
		lipgloss.Center,
		lipgloss.Center,
		content,
	)
}
