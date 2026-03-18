package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/p-society/raag/rpc"
)

var (
	header          = "══════════════ RAAG ══════════════"
	helpFooter      = "\n[q]uit [tab]view [space]play/pause [n]ext [p]rev [+/-]vol"
	helpFooterLib   = helpFooter + " [/]search"
	maxLibraryItems = 20
	tickInterval    = 2 * time.Second
	fetchTimeout    = 1 * time.Second
	actionTimeout   = 2 * time.Second
)

func Start(client *rpc.Client) error {
	m := NewRPCModel(client)
	program := tea.NewProgram(m, tea.WithAltScreen())
	_, err := program.Run()
	return err
}

type RPCModel struct {
	client       *rpc.Client
	keys         KeyMap
	searchInput  textinput.Model
	state        *DaemonState
	currentView  View
	selectedIdx  int
	needsRefresh bool
}

type DaemonState struct {
	NowPlaying *rpc.NowPlayingResult
	Library    *rpc.LibraryListResult
	Network    *rpc.NetworkStatusResult
	Status     *rpc.Status
}

func NewRPCModel(client *rpc.Client) *RPCModel {
	searchInput := textinput.New()
	searchInput.Placeholder = "Search library..."
	searchInput.Prompt = "/ "

	return &RPCModel{
		client:      client,
		keys:        DefaultKeyMap(),
		searchInput: searchInput,
		currentView: ViewPlayer,
		state:       &DaemonState{},
	}
}

type (
	tickMsg        time.Time
	stateUpdateMsg *DaemonState
)

func (m *RPCModel) Init() tea.Cmd {
	return tea.Batch(m.fetchState(), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *RPCModel) fetchState() tea.Cmd {
	return func() tea.Msg {
		var fs rpc.FullStateResult
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		if err := m.client.CallWithContext(ctx, "DaemonService.GetFullState", &rpc.EmptyArgs{}, &fs); err != nil {
			return nil
		}
		state := &DaemonState{
			NowPlaying: fs.NowPlaying,
			Network:    fs.Network,
			Status:     fs.Status,
			Library:    fs.Library,
		}
		return stateUpdateMsg(state)
	}
}

func (m *RPCModel) fetchPlayerState() tea.Cmd {
	return func() tea.Msg {
		var fs rpc.FullStateResult
		ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
		defer cancel()

		if err := m.client.CallWithContext(ctx, "DaemonService.GetFullState", &rpc.EmptyArgs{}, &fs); err != nil {
			return nil
		}
		state := &DaemonState{
			NowPlaying: fs.NowPlaying,
			Network:    fs.Network,
			Status:     fs.Status,
		}
		return stateUpdateMsg(state)
	}
}

func (m *RPCModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		m.needsRefresh = true
		return m.handleKey(msg)
	case tickMsg:
		if m.needsRefresh {
			m.needsRefresh = false
			switch m.currentView {
			case ViewLibrary:
				return m, tea.Batch(m.fetchState(), tickCmd())
			default:
				return m, tea.Batch(m.fetchPlayerState(), tickCmd())
			}
		}
		return m, tickCmd()
	case stateUpdateMsg:
		if msg == nil {
			return m, nil
		}
		m.state = msg
		return m, nil
	}
	return m, nil
}

func (m *RPCModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := m.keys
	if m.currentView == ViewLibrary && m.searchInput.Focused() {
		if _, matched := MatchAny(msg, k.Enter, k.Up, k.Down, k.Tab, k.Quit); matched {
			// Let main switch handle navigation keys
		} else {
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			return m, cmd
		}
	}

	switch {
	case key.Matches(msg, k.Quit):
		if m.currentView == ViewPlayer {
			return m, tea.Quit
		}

		m.currentView = ViewPlayer
		m.searchInput.Blur()
		return m, nil
	case key.Matches(msg, k.Tab):
		m.nextView()
		return m, nil
	case key.Matches(msg, k.ViewPlayer):
		m.currentView = ViewPlayer
		m.selectedIdx = 0
		return m, nil
	case key.Matches(msg, k.ViewLibrary):
		m.currentView = ViewLibrary
		m.selectedIdx = 0
		return m, nil
	case key.Matches(msg, k.ViewPlaylist):
		m.currentView = ViewPlaylist
		m.selectedIdx = 0
		return m, nil
	case key.Matches(msg, k.ViewPeers):
		m.currentView = ViewPeers
		m.selectedIdx = 0
		return m, nil
	case key.Matches(msg, k.Help):
		m.currentView = ViewHelp
		m.searchInput.Blur()
		return m, nil
	case key.Matches(msg, k.Search) && m.currentView == ViewLibrary:
		m.searchInput.Focus()
		m.searchInput.SetValue("")
		return m, nil
	case key.Matches(msg, k.Up) && m.selectedIdx > 0:
		m.selectedIdx--
		return m, nil
	case key.Matches(msg, k.Down):
		m.selectedIdx++
		return m, nil
	case key.Matches(msg, k.Enter) && m.currentView == ViewLibrary:
		if m.state.Library != nil && m.selectedIdx < len(m.state.Library.Songs) {
			song := m.state.Library.Songs[m.selectedIdx]
			ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
			defer cancel()

			err := m.client.CallWithContext(ctx, "DaemonService.Play", &rpc.PlayArgs{Song: song.Title}, &rpc.EmptyResult{})
			if err != nil {
				m.state.Status = nil
			}
			return m, m.fetchPlayerState()
		}
		return m, nil
	}

	if m.currentView == ViewPlayer {
		if key.Matches(msg, k.PlayPause) {
			if m.state.NowPlaying != nil && m.state.NowPlaying.Playing {
				return m.executeAction("DaemonService.Pause", &rpc.EmptyArgs{})
			}
			return m.executeAction("DaemonService.Resume", &rpc.EmptyArgs{})
		}
		if key.Matches(msg, k.Next) {
			return m.executeAction("DaemonService.Next", &rpc.EmptyArgs{})
		}
		if key.Matches(msg, k.Prev) {
			return m.executeAction("DaemonService.Previous", &rpc.EmptyArgs{})
		}
		if key.Matches(msg, k.VolUp) {
			return m.executeAction("DaemonService.SetVolume", &rpc.VolumeArgs{Level: 5})
		}
		if key.Matches(msg, k.VolDown) {
			return m.executeAction("DaemonService.SetVolume", &rpc.VolumeArgs{Level: -5})
		}
	}
	return m, nil
}

func (m *RPCModel) executeAction(method string, args any) (tea.Model, tea.Cmd) {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	var result rpc.EmptyResult
	if err := m.client.CallWithContext(ctx, method, args, &result); err != nil {
		m.state.Status = nil
	}
	return m, m.fetchPlayerState()
}

func (m *RPCModel) nextView() {
	switch m.currentView {
	case ViewPlayer:
		m.currentView = ViewLibrary
	case ViewLibrary:
		m.currentView = ViewPlaylist
	case ViewPlaylist:
		m.currentView = ViewPeers
	case ViewPeers:
		m.currentView = ViewPlayer
	default:
		m.currentView = ViewPlayer
	}
	m.selectedIdx = 0
}

func (m *RPCModel) View() string {
	if m.state == nil {
		return TitleStyle.Render("Connecting to daemon...")
	}
	if m.state.Status == nil {
		return ErrorStyle.Render("Daemon not responding. Start it with: raag daemon")
	}

	var b strings.Builder
	b.WriteString(TitleStyle.Render(header))
	b.WriteString("\n\n")

	switch m.currentView {
	case ViewPlayer:
		m.renderPlayerView(&b)
	case ViewLibrary:
		m.renderLibraryView(&b)
	case ViewPlaylist:
		m.renderPlaylistView(&b)
	case ViewPeers:
		m.renderPeersView(&b)
	case ViewHelp:
		m.renderHelpView(&b)
	}

	if m.currentView == ViewLibrary {
		b.WriteString(HelpStyle.Render(helpFooterLib))
	} else {
		b.WriteString(HelpStyle.Render(helpFooter))
	}
	return b.String()
}

func (m *RPCModel) renderPlayerView(b *strings.Builder) {
	if m.state.NowPlaying != nil && m.state.NowPlaying.Title != "" {
		np := m.state.NowPlaying
		status := "Playing"
		if np.Paused {
			status = "Paused"
		} else if !np.Playing {
			status = "Stopped"
		}

		b.WriteString(LabelStyle.Render("Now Playing: "))
		b.WriteString(StatusOnlineStyle.Render(status))
		b.WriteString("\n")
		b.WriteString(LabelStyle.Render("Title:  "))
		b.WriteString(ValueStyle.Render(np.Title))
		b.WriteString("\n")
		b.WriteString(LabelStyle.Render("Artist: "))
		b.WriteString(ValueStyle.Render(np.Artist))
		b.WriteString("\n")
		b.WriteString(LabelStyle.Render("Album:  "))
		b.WriteString(ValueStyle.Render(np.Album))
		b.WriteString("\n")

		if np.Duration > 0 {
			progress := float64(np.Position) / float64(np.Duration)
			barWidth := 30
			filled := int(progress * float64(barWidth))
			bar := ProgressBarStyle.Render(strings.Repeat("█", filled)) + ProgressEmptyStyle.Render(strings.Repeat("─", barWidth-filled))
			b.WriteString("\n")
			b.WriteString(bar)
			fmt.Fprintf(b, " %s/%s", formatTime(np.Position), formatTime(np.Duration))
			b.WriteString("\n")
		}

		volLevel := int(np.Volume) / 10
		volBar := HighlightStyle.Render(strings.Repeat("█", volLevel)) + SubtitleStyle.Render(strings.Repeat("░", 10-volLevel))
		b.WriteString(LabelStyle.Render("Volume: "))
		b.WriteString(volBar)
		fmt.Fprintf(b, " %.0f%%\n", np.Volume)
	} else {
		b.WriteString(SubtitleStyle.Render("No song playing"))
	}
}

func (m *RPCModel) renderLibraryView(b *strings.Builder) {
	b.WriteString(LabelStyle.Render("Search: "))
	b.WriteString(m.searchInput.View())
	b.WriteString("\n\n")
	if m.state.Library == nil {
		b.WriteString(SubtitleStyle.Render("Loading library...\n"))
		return
	}
	if len(m.state.Library.Songs) == 0 {
		b.WriteString(SubtitleStyle.Render("Library is empty\n"))
		return
	}

	b.WriteString(LabelStyle.Render(fmt.Sprintf("Library: %d songs\n\n", len(m.state.Library.Songs))))
	filter := m.searchInput.Value()
	for i, song := range m.state.Library.Songs {
		if i >= maxLibraryItems {
			b.WriteString(SubtitleStyle.Render(fmt.Sprintf("... and %d more\n", len(m.state.Library.Songs)-maxLibraryItems)))
			break
		}
		if filter != "" && !strings.Contains(strings.ToLower(song.Title), strings.ToLower(filter)) &&
			!strings.Contains(strings.ToLower(song.Artist), strings.ToLower(filter)) {
			continue
		}
		if i == m.selectedIdx {
			b.WriteString(ListSelectedStyle.Render(fmt.Sprintf("  %s - %s", song.Title, song.Artist)))
		} else {
			b.WriteString(ListItemStyle.Render(fmt.Sprintf("  %s - %s", song.Title, song.Artist)))
		}
		b.WriteString("\n")
	}
}

func (m *RPCModel) renderPeersView(b *strings.Builder) {
	if m.state.Network == nil {
		b.WriteString(SubtitleStyle.Render("Network status loading...\n"))
		return
	}

	ns := m.state.Network.State
	b.WriteString(LabelStyle.Render("Self: "))
	b.WriteString(ValueStyle.Render(ns.SelfID))
	b.WriteString("\n")
	b.WriteString(LabelStyle.Render("DHT: "))
	b.WriteString(ValueStyle.Render(fmt.Sprintf("%v (peers: %d)", ns.DHTEnabled, ns.DHTPeers)))
	b.WriteString("\n")
	b.WriteString(LabelStyle.Render("Connected Peers: "))
	b.WriteString(ValueStyle.Render(fmt.Sprintf("%d", len(ns.ConnectedPeers))))
	b.WriteString("\n")
	b.WriteString(LabelStyle.Render("Known Peers: "))
	b.WriteString(ValueStyle.Render(fmt.Sprintf("%d", len(ns.KnownPeers))))
	b.WriteString("\n")
}

func (m *RPCModel) renderPlaylistView(b *strings.Builder) {
	b.WriteString(TitleStyle.Render("Playlists:\n"))
	b.WriteString(SubtitleStyle.Render("(manage via CLI commands: raag playlist list/add/remove)\n"))
}

func (m *RPCModel) renderHelpView(b *strings.Builder) {
	b.WriteString(TitleStyle.Render("Keyboard Shortcuts:\n\n"))
	b.WriteString(LabelStyle.Render("  q / Ctrl+C: "))
	b.WriteString(ValueStyle.Render("quit\n"))
	b.WriteString(LabelStyle.Render("  Tab:        "))
	b.WriteString(ValueStyle.Render("next view\n"))
	b.WriteString(LabelStyle.Render("  Space:      "))
	b.WriteString(ValueStyle.Render("play/pause\n"))
	b.WriteString(LabelStyle.Render("  n:          "))
	b.WriteString(ValueStyle.Render("next song\n"))
	b.WriteString(LabelStyle.Render("  p:          "))
	b.WriteString(ValueStyle.Render("previous song\n"))
	b.WriteString(LabelStyle.Render("  +/-:        "))
	b.WriteString(ValueStyle.Render("volume up/down\n"))
	b.WriteString(LabelStyle.Render("  /:          "))
	b.WriteString(ValueStyle.Render("search library\n"))
	b.WriteString(LabelStyle.Render("  1-4:        "))
	b.WriteString(ValueStyle.Render("switch views\n"))
}

func formatTime(seconds int) string {
	return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
}
