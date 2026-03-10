package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/library"
	"github.com/p-society/raag/internal/metadata"
	"github.com/p-society/raag/internal/network"
	"github.com/p-society/raag/internal/player"
	"github.com/p-society/raag/internal/playlist"
)

type View string

const (
	ViewPlayer   View = "player"
	ViewLibrary  View = "library"
	ViewPlaylist View = "playlist"
	ViewPeers    View = "peers"
	ViewHelp     View = "help"
)

type Model struct {
	player    *player.Player
	library   *library.Library
	network   *network.NetworkManager
	playlists *playlist.Manager

	currentView  View
	selectedIdx  int
	searchQuery  string
	librarySongs []metadata.Song
	filteredLib  []metadata.Song

	peerList []peer.AddrInfo
}

func NewModel(
	lib *library.Library,
	p *player.Player,
	net *network.NetworkManager,
	pm *playlist.Manager,
) *Model {
	m := &Model{
		player:      p,
		library:     lib,
		network:     net,
		playlists:   pm,
		currentView: ViewPlayer,
		selectedIdx: 0,
	}

	m.librarySongs = lib.ListSongs()
	m.filteredLib = m.librarySongs
	m.peerList = net.GetPeers()

	net.OnPeerJoin = func(id peer.ID) {
		m.peerList = m.network.GetPeers()
	}
	net.OnPeerLeave = func(id peer.ID) {
		m.peerList = m.network.GetPeers()
	}

	return m
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		return m, nil
	}
	return m, nil
}

func (m *Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		if m.currentView == ViewPlayer {
			return m, tea.Quit
		}
		m.currentView = ViewPlayer
		m.searchQuery = ""
	case "tab":
		m.nextView()
	case "1":
		m.currentView = ViewPlayer
		m.selectedIdx = 0
	case "2":
		m.currentView = ViewLibrary
		m.selectedIdx = 0
	case "3":
		m.currentView = ViewPlaylist
		m.selectedIdx = 0
	case "4":
		m.currentView = ViewPeers
		m.selectedIdx = 0
	case " ":
		if m.currentView == ViewPlayer {
			if m.player.IsPlaying() {
				m.player.Pause()
			} else if m.player.IsPaused() {
				m.player.Resume()
			} else if len(m.player.GetQueue()) > 0 {
				m.player.PlayQueue()
			}
		}
	case "n":
		if m.currentView == ViewPlayer {
			m.player.Next()
		}
	case "p":
		if m.currentView == ViewPlayer {
			m.player.Previous()
		}
	case "+", "=":
		if m.currentView == ViewPlayer {
			m.player.VolumeUp(5)
		}
	case "-":
		if m.currentView == ViewPlayer {
			m.player.VolumeDown(5)
		}
	case "up", "k":
		if m.selectedIdx > 0 {
			m.selectedIdx--
		}
	case "down", "j":
		m.handleScrollDown()
	case "enter":
		m.handleEnter()
	case "backspace":
		if len(m.searchQuery) > 0 {
			m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
			m.filterLibrary()
		}
	case "/":
		m.searchQuery = ""
	default:
		if len(msg.String()) == 1 {
			m.searchQuery += msg.String()
			m.filterLibrary()
		}
	}

	return m, nil
}

func (m *Model) nextView() {
	switch m.currentView {
	case ViewPlayer:
		m.currentView = ViewLibrary
	case ViewLibrary:
		m.currentView = ViewPlaylist
	case ViewPlaylist:
		m.currentView = ViewPeers
	case ViewPeers:
		m.currentView = ViewPlayer
	}
	m.selectedIdx = 0
}

func (m *Model) handleScrollDown() {
	m.selectedIdx++
}

func (m *Model) handleEnter() {
	switch m.currentView {
	case ViewLibrary:
		if m.selectedIdx < len(m.filteredLib) {
			song := m.filteredLib[m.selectedIdx]
			m.player.AddToQueue(song)
			if m.player.GetCurrentSong() == nil {
				m.player.PlayQueue()
			}
		}
	case ViewPlaylist:
		m.currentView = ViewPlayer
	}
}

func (m *Model) filterLibrary() {
	if m.searchQuery == "" {
		m.filteredLib = m.librarySongs
		return
	}

	m.filteredLib = nil
	lowerQuery := fmt.Sprintf("%s", m.searchQuery)
	for _, song := range m.librarySongs {
		if containsFold(song.Title, lowerQuery) ||
			containsFold(song.Artist, lowerQuery) ||
			containsFold(song.Album, lowerQuery) {
			m.filteredLib = append(m.filteredLib, song)
		}
	}
	if m.selectedIdx >= len(m.filteredLib) {
		m.selectedIdx = max(0, len(m.filteredLib)-1)
	}
}

func containsFold(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}

	c := []rune(substr)
	for i := 0; i <= len(s)-len(c); i++ {
		if equalFold(s[i:i+len(c)], c) {
			return true
		}
	}
	return false
}

func equalFold(s string, c []rune) bool {
	for i := range c {
		r := rune(s[i])
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		if r != c[i] {
			return false
		}
	}
	return true
}

func (m *Model) View() string {
	switch m.currentView {
	case ViewPlayer:
		return m.renderPlayerView()
	case ViewLibrary:
		return m.renderLibraryView()
	case ViewPlaylist:
		return m.renderPlaylistView()
	case ViewPeers:
		return m.renderPeersView()
	case ViewHelp:
		return m.renderHelpView()
	}
	return ""
}

func (m *Model) renderPlayerView() string {
	song := m.player.GetCurrentSong()
	pos := m.player.GetPosition()
	dur := m.player.GetDuration()
	vol := m.player.GetVolume()
	playing := m.player.IsPlaying()
	paused := m.player.IsPaused()

	status := "Stopped"
	if playing {
		status = "Playing"
	} else if paused {
		status = "Paused"
	}

	progress := renderProgressBar(pos, dur)
	output := TitleStyle.Width(60).Render("Now Playing")
	output += "\n\n"

	if song != nil {
		output += LabelStyle.Render("Title:") + " " + ValueStyle.Render(song.Title) + "\n"
		output += LabelStyle.Render("Artist:") + " " + ValueStyle.Render(song.Artist) + "\n"
		output += LabelStyle.Render("Album:") + " " + ValueStyle.Render(song.Album) + "\n"
	} else {
		output += ValueStyle.Render("No song playing") + "\n"
	}

	output += "\n"
	output += LabelStyle.Render("Status:") + " " + ValueStyle.Render(status) + "\n"
	output += LabelStyle.Render("Progress:") + " " + progress + "\n"
	output += LabelStyle.Render("Volume:") + " " + renderVolumeBar(vol) + "\n"

	output += "\n"
	output += HelpStyle.Render("[Space] Play/Pause  [n] Next  [p] Previous  [+/-] Volume")
	output += "\n"
	output += HelpStyle.Render("[Tab] Switch View  [1-4] Views  [q] Quit")

	return BorderStyle.Height(15).Render(output)
}

func (m *Model) renderLibraryView() string {
	var output strings.Builder
	output.WriteString(TitleStyle.Width(60).Render("Library"))
	output.WriteString("\n\n")

	if m.searchQuery != "" {
		output.WriteString(LabelStyle.Render("Search:") + " " + ValueStyle.Render(m.searchQuery+"_") + "\n\n")
	} else {
		output.WriteString(LabelStyle.Render("Search:") + " " + HelpStyle.Render("(press / to search)") + "\n\n")
	}

	for i, song := range m.filteredLib {
		if i == m.selectedIdx && m.currentView == ViewLibrary {
			output.WriteString(ListSelectedStyle.Render("> "+song.Title+" - "+song.Artist) + "\n")
		} else {
			output.WriteString(ListItemStyle.Render("  "+song.Title+" - "+song.Artist) + "\n")
		}
	}
	if len(m.filteredLib) == 0 {
		output.WriteString(HelpStyle.Render("No songs found"))
	}

	output.WriteString("\n")
	output.WriteString(HelpStyle.Render("[Enter] Add to Queue  [j/k] Navigate  [q] Back"))
	return BorderStyle.Height(20).Render(output.String())
}

func (m *Model) renderPlaylistView() string {
	var output strings.Builder
	output.WriteString(TitleStyle.Width(60).Render("Playlists"))
	output.WriteString("\n\n")

	playlists := m.playlists.List()
	for i, name := range playlists {
		if i == m.selectedIdx && m.currentView == ViewPlaylist {
			output.WriteString(ListSelectedStyle.Render("> "+name) + "\n")
		} else {
			output.WriteString(ListItemStyle.Render("  "+name) + "\n")
		}
	}

	if len(playlists) == 0 {
		output.WriteString(HelpStyle.Render("No playlists. Use CLI to create one."))
	}

	output.WriteString("\n")
	output.WriteString(HelpStyle.Render("[Enter] Play  [j/k] Navigate  [q] Back"))
	return BorderStyle.Height(15).Render(output.String())
}

func (m *Model) renderPeersView() string {
	online := m.network.IsOnline()
	peerID := m.network.GetPeerID()
	multiaddr := m.network.GetMultiaddr()

	var output strings.Builder
	output.WriteString(TitleStyle.Width(60).Render("Peers"))
	output.WriteString("\n\n")

	if online {
		output.WriteString(LabelStyle.Render("Status:") + " " + StatusOnlineStyle.Render("Online") + "\n")
	} else {
		output.WriteString(LabelStyle.Render("Status:") + " " + StatusOfflineStyle.Render("Offline") + "\n")
	}

	output.WriteString(LabelStyle.Render("Your Peer ID:") + " " + ValueStyle.Render(peerID.String()) + "\n")
	output.WriteString(LabelStyle.Render("Multiaddr:") + " " + ValueStyle.Render(multiaddr) + "\n\n")
	output.WriteString(LabelStyle.Render("Connected Peers:") + "\n")
	if len(m.peerList) == 0 {
		output.WriteString(HelpStyle.Render("  No peers connected"))
	} else {
		for i, p := range m.peerList {
			if i == m.selectedIdx && m.currentView == ViewPeers {
				output.WriteString(ListSelectedStyle.Render("> "+p.ID.String()) + "\n")
			} else {
				output.WriteString(ListItemStyle.Render("  "+p.ID.String()) + "\n")
			}
		}
	}

	output.WriteString("\n")
	output.WriteString(HelpStyle.Render("[Tab] Switch View  [q] Back"))
	return BorderStyle.Height(15).Render(output.String())
}

func (m *Model) renderHelpView() string {
	output := TitleStyle.Width(60).Render("Help")
	output += "\n\n"

	output += LabelStyle.Render("Keyboard Shortcuts:") + "\n"
	output += "  [Space]  Play/Pause\n"
	output += "  [n]      Next song\n"
	output += "  [p]      Previous song\n"
	output += "  [+]      Volume up\n"
	output += "  [-]      Volume down\n"
	output += "  [Tab]    Switch views\n"
	output += "  [1-4]    Direct view selection\n"
	output += "  [j/k]    Scroll up/down\n"
	output += "  [Enter]  Select/Play\n"
	output += "  [/]      Search (in library)\n"
	output += "  [q]      Quit/Back\n\n"

	output += LabelStyle.Render("Views:") + "\n"
	output += "  [1] Player   - Now playing, controls\n"
	output += "  [2] Library - Browse songs\n"
	output += "  [3] Playlist - Manage playlists\n"
	output += "  [4] Peers   - Network status\n\n"

	output += HelpStyle.Render("[q] Back to Player")
	return BorderStyle.Height(20).Render(output)
}

func renderProgressBar(current, total int) string {
	if total == 0 {
		return "[--------------------] 0:00 / 0:00"
	}

	barWidth := 20
	progress := float64(current) / float64(total)
	filled := int(progress * float64(barWidth))

	var bar strings.Builder
	for i := range barWidth {
		if i < filled {
			bar.WriteString("█")
		} else {
			bar.WriteString("─")
		}
	}
	return ProgressBarStyle.Render("["+bar.String()+"]") + " " + formatTime(current) + " / " + formatTime(total)
}

func renderVolumeBar(vol float64) string {
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}

	barWidth := 10
	filled := int((vol / 100) * float64(barWidth))

	var bar strings.Builder
	for i := range barWidth {
		if i < filled {
			bar.WriteString("█")
		} else {
			bar.WriteString("─")
		}
	}
	return ProgressBarStyle.Render("["+bar.String()+"]") + " " + formatVolume(vol)
}

func formatTime(seconds int) string {
	mins := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%d:%02d", mins, secs)
}

func formatVolume(vol float64) string {
	return fmt.Sprintf("%.0f%%", vol)
}
