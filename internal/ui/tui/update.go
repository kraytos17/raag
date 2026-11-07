package tui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"

	pb "github.com/p-society/raag/proto/gen"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.relayout(msg.Width, msg.Height)
	case tea.KeyPressMsg:
		cmds = append(cmds, m.handleKey(msg)...)
	case PlaybackStateMsg:
		m.Player.State = PlaybackState(msg.State)
	case TrackChangedMsg:
		m.handleTrackChanged(msg)
	case ProgressMsg:
		m.Player.PositionMs = msg.PositionMs
		if msg.DurationMs > 0 {
			m.Player.DurationMs = msg.DurationMs
		}
	case VolumeMsg:
		m.Volume = msg.Volume
	case QueueUpdatedMsg:
		m.setQueueItems(msg.Tracks)
	case PeerConnectedMsg:
		cmds = append(cmds, m.handlePeerConnected(msg)...)
	case PeerScoreUpdatedMsg:
		cmds = append(cmds, m.handlePeerScoreUpdated(msg)...)
	case PeerDisconnectedMsg:
		cmds = append(cmds, m.handlePeerDisconnected(msg)...)
	case LibraryUpdatedMsg:
		cmds = append(cmds, m.fetchLibrary())
	case ReconnectingMsg:
		m.Reconnecting = true
		m.Connected = false
	case ConnectedMsg:
		cmds = append(cmds, m.handleConnected()...)
	case ErrorMsg:
		if msg.Err != "" {
			m.Err = fmt.Errorf("%s", msg.Err)
		}
	case StatusMsg:
		cmds = append(cmds, m.handleStatusMsg(msg)...)
	case LibraryMsg:
		cmds = append(cmds, m.handleLibraryMsg(msg)...)
	case QueueMsg:
		cmds = append(cmds, m.handleQueueMsg(msg)...)
	case QueueModeMsg:
		if msg.Err == nil {
			m.Player.Shuffle = msg.Shuffle
			m.Player.RepeatMode = msg.Repeat
		}
	case PeersMsg:
		cmds = append(cmds, m.handlePeersMsg(msg)...)
	case PlaylistsMsg:
		cmds = append(cmds, m.handlePlaylistsMsg(msg)...)
	case PlaylistTracksMsg:
		cmds = append(cmds, m.handlePlaylistTracksMsg(msg)...)
	}

	cmds = append(cmds, m.updateChild(msg)...)
	return m, tea.Batch(cmds...)
}

func (m *Model) handleTrackChanged(msg TrackChangedMsg) {
	m.Player.CurrentTrack = msg.Track
	m.Player.PositionMs = 0
	if msg.Track != nil {
		m.Player.DurationMs = int64(msg.Track.DurationMs)
	}
}

func (m *Model) handlePeerConnected(msg PeerConnectedMsg) []tea.Cmd {
	if msg.Peer != nil {
		m.PeerCount++
		return []tea.Cmd{m.fetchPeers()}
	}
	return nil
}

// handlePeerScoreUpdated refreshes a peer's score without treating it as a new
// connection (no PeerCount change).
func (m *Model) handlePeerScoreUpdated(_ PeerScoreUpdatedMsg) []tea.Cmd {
	return []tea.Cmd{m.fetchPeers()}
}

func (m *Model) handlePeerDisconnected(_ PeerDisconnectedMsg) []tea.Cmd {
	if m.PeerCount > 0 {
		m.PeerCount--
	}
	return []tea.Cmd{m.fetchPeers()}
}

func (m *Model) handleConnected() []tea.Cmd {
	m.Reconnecting = false
	m.Connected = true
	return []tea.Cmd{
		m.fetchInitialData(),
		m.fetchLibrary(),
		m.fetchQueue(),
		m.fetchQueueMode(),
		m.fetchPeers(),
		m.fetchPlaylists(),
	}
}

func (m *Model) handleStatusMsg(msg StatusMsg) []tea.Cmd {
	if msg.Err != nil {
		m.Err = msg.Err
		return nil
	}
	if msg.Status != nil {
		m.Player.State = PlaybackState(msg.Status.State)
		m.Player.CurrentTrack = msg.Status.CurrentTrack
		m.Player.PositionMs = msg.Status.PositionMs
		if msg.Status.CurrentTrack != nil {
			m.Player.DurationMs = int64(msg.Status.CurrentTrack.DurationMs)
		}

		m.Player.QueueLen = msg.Status.QueueLength
		m.Player.QueuePos = msg.Status.QueuePosition
		m.Player.BufferFill = float64(msg.Status.BufferFill)
		m.Volume = msg.Status.Volume
	}

	m.Err = nil
	m.Connected = true
	return nil
}

func (m *Model) handleLibraryMsg(msg LibraryMsg) []tea.Cmd {
	if msg.Err != nil {
		m.Err = msg.Err
		return nil
	}

	m.Err = nil
	items := make([]list.Item, len(msg.Tracks))
	for i, t := range msg.Tracks {
		items[i] = TrackItem{Track: t}
	}

	m.Library.SetItems(items)
	return nil
}

func (m *Model) handleQueueMsg(msg QueueMsg) []tea.Cmd {
	if msg.Err != nil {
		m.Err = msg.Err
		return nil
	}

	m.setQueueItems(msg.Tracks)
	return nil
}

func (m *Model) handlePeersMsg(msg PeersMsg) []tea.Cmd {
	if msg.Err != nil {
		m.Err = msg.Err
		return nil
	}

	items := make([]list.Item, len(msg.Peers))
	for i, p := range msg.Peers {
		items[i] = PeerItem{Peer: p}
	}

	m.Peers.SetItems(items)
	m.PeerCount = len(msg.Peers)
	return nil
}

func (m *Model) handlePlaylistsMsg(msg PlaylistsMsg) []tea.Cmd {
	if msg.Err != nil {
		m.Err = msg.Err
		return nil
	}

	items := make([]list.Item, len(msg.Playlists))
	for i, p := range msg.Playlists {
		items[i] = PlaylistItem{Playlist: p}
	}

	m.Playlists.SetItems(items)
	return nil
}

func (m *Model) handlePlaylistTracksMsg(msg PlaylistTracksMsg) []tea.Cmd {
	if msg.Err != nil {
		m.Err = msg.Err
		return nil
	}

	items := make([]list.Item, len(msg.Tracks))
	for i, t := range msg.Tracks {
		items[i] = TrackItem{Track: t}
	}

	m.Library.SetItems(items)
	m.Library.Title = " ▶ " + msg.Playlist.Name + " "
	return nil
}

func (m *Model) updateChild(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd
	if m.InSearch {
		newInput, cmd := m.SearchInput.Update(msg)
		m.SearchInput = newInput
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	activeList := m.activeList()
	if activeList != nil {
		newList, cmd := activeList.Update(msg)
		*activeList = newList
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

func (m *Model) setQueueItems(tracks []*pb.Track) {
	items := make([]list.Item, len(tracks))
	for i, t := range tracks {
		items[i] = QueueItem{Track: t, Index: i, IsCurrent: i == int(m.Player.QueuePos)}
	}

	m.Queue.SetItems(items)
	m.Player.QueueLen = int32(len(tracks))
}

// handleKey processes key presses and returns the resulting commands.
func (m *Model) handleKey(msg tea.KeyPressMsg) []tea.Cmd {
	var cmds []tea.Cmd
	keys := DefaultKeyMap()
	if m.InSearch {
		return m.handleSearchKey(msg)
	}
	if m.ShowHelp {
		if key.Matches(msg, keys.Help) {
			m.ShowHelp = false
		}
		return cmds
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m.handleQuit()
	case key.Matches(msg, keys.Help):
		m.ShowHelp = !m.ShowHelp
	case key.Matches(msg, keys.Lyrics):
		m.ShowLyrics = !m.ShowLyrics
	case key.Matches(msg, keys.Eq):
		m.ShowEq = !m.ShowEq
	case key.Matches(msg, keys.EqCycle):
		cmds = append(cmds, m.cmdCycleEQ())
	case key.Matches(msg, keys.Tab):
		m.ActivePanel = (m.ActivePanel + 1) % 4
	case key.Matches(msg, keys.Search):
		m.InSearch = true
		m.SearchInput.Focus()
	case key.Matches(msg, keys.PlayPause), key.Matches(msg, keys.Next),
		key.Matches(msg, keys.Prev), key.Matches(msg, keys.Stop),
		key.Matches(msg, keys.SeekFwd), key.Matches(msg, keys.SeekBack),
		key.Matches(msg, keys.VolUp), key.Matches(msg, keys.VolDown):
		cmds = append(cmds, m.handleTransportKey(msg, keys)...)
	case key.Matches(msg, keys.Enter), key.Matches(msg, keys.Delete):
		cmds = append(cmds, m.handlePanelKey(msg, keys)...)
	case key.Matches(msg, keys.Refresh):
		cmds = append(cmds, m.fetchLibrary(), m.fetchQueue(), m.fetchQueueMode(), m.fetchPeers(), m.fetchPlaylists())
	case key.Matches(msg, keys.Shuffle):
		cmds = append(cmds, m.cmdQueueShuffle())
	case key.Matches(msg, keys.Repeat):
		cmds = append(cmds, m.cmdQueueRepeat())
	}
	return cmds
}

// handleTransportKey maps playback transport keys (play/pause, next/prev,
// stop, seek, volume) to commands.
func (m *Model) handleTransportKey(msg tea.KeyPressMsg, keys KeyMap) []tea.Cmd {
	switch {
	case key.Matches(msg, keys.PlayPause):
		if m.Player.State == StatePlaying {
			return []tea.Cmd{m.cmdPause()}
		}
		return []tea.Cmd{m.cmdResume()}
	case key.Matches(msg, keys.Next):
		return []tea.Cmd{m.cmdNext()}
	case key.Matches(msg, keys.Prev):
		return []tea.Cmd{m.cmdPrev()}
	case key.Matches(msg, keys.Stop):
		return []tea.Cmd{m.cmdStop()}
	case key.Matches(msg, keys.SeekFwd):
		return []tea.Cmd{m.cmdSeek(10000)}
	case key.Matches(msg, keys.SeekBack):
		return []tea.Cmd{m.cmdSeek(-10000)}
	case key.Matches(msg, keys.VolUp):
		return []tea.Cmd{m.cmdVolume(5)}
	case key.Matches(msg, keys.VolDown):
		return []tea.Cmd{m.cmdVolume(-5)}
	}
	return nil
}

// handlePanelKey maps panel-scoped keys (Enter, Delete) to commands.
func (m *Model) handlePanelKey(msg tea.KeyPressMsg, keys KeyMap) []tea.Cmd {
	switch {
	case key.Matches(msg, keys.Enter):
		if m.ActivePanel == PanelPlaylists {
			return []tea.Cmd{m.cmdShowSelectedPlaylist()}
		}
		return []tea.Cmd{m.cmdPlaySelected()}
	case key.Matches(msg, keys.Delete):
		if m.ActivePanel == PanelQueue {
			return []tea.Cmd{m.cmdQueueRemove()}
		}
	}
	return nil
}

// handleQuit tears down the IPC subscription and client before quitting so the
// connection and keepalive goroutine don't leak until process exit
func (m *Model) handleQuit() []tea.Cmd {
	if m.EventCh != nil {
		m.EventCh.Close()
	}
	if m.IPCClient != nil {
		m.IPCClient.Close()
	}
	return []tea.Cmd{tea.Quit}
}

// handleSearchKey processes keys while the search input is focused.
func (m *Model) handleSearchKey(msg tea.KeyPressMsg) []tea.Cmd {
	switch msg.String() {
	case "esc":
		m.InSearch = false
		m.SearchInput.Blur()
	case "enter":
		m.InSearch = false
		m.SearchInput.Blur()
		query := m.SearchInput.Value()
		if query != "" {
			return []tea.Cmd{m.cmdSearch(query)}
		}
	}
	return nil
}

func (m *Model) activeList() *list.Model {
	switch m.ActivePanel {
	case PanelLibrary:
		return &m.Library
	case PanelQueue:
		return &m.Queue
	case PanelPeers:
		return &m.Peers
	case PanelPlaylists:
		return &m.Playlists
	}
	return nil
}
