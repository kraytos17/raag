package tui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"google.golang.org/protobuf/proto"

	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/ipc"
	pb "github.com/p-society/raag/proto/gen"
)

type Panel int

const (
	PanelLibrary Panel = iota
	PanelQueue
	PanelPeers
	PanelPlaylists
)

type PlaybackState string

const (
	StateIdle      PlaybackState = "idle"
	StatePlaying   PlaybackState = "playing"
	StatePaused    PlaybackState = "paused"
	StateBuffering PlaybackState = "buffering"
	StateError     PlaybackState = "error"
)

const unknownTitle = "Unknown"

const (
	repeatNone = "none"
	repeatAll  = "all"
	repeatOne  = "one"
)

// Model is the root Bubble Tea model for the Raag TUI.
type Model struct {
	SocketPath string
	IPCClient  *ipc.Client
	EventCh    *ipc.EventClient
	Program    *tea.Program

	Width  int
	Height int

	ActivePanel Panel

	Library   list.Model
	Queue     list.Model
	Peers     list.Model
	Playlists list.Model

	SearchInput textinput.Model
	InSearch    bool

	HelpViewport viewport.Model
	ShowHelp     bool
	ShowLyrics   bool
	ShowEq       bool
	EQ           domain.EqualizerSettings

	Player PlayerState
	Err    error

	Connected    bool
	Reconnecting bool
	PeerCount    int
	Volume       int32
}

type PlayerState struct {
	State        PlaybackState
	CurrentTrack *pb.Track
	PositionMs   int64
	DurationMs   int64
	QueueLen     int32
	QueuePos     int32
	Shuffle      bool
	RepeatMode   string
	BufferFill   float64
}

type TrackItem struct {
	Track *pb.Track
}

func (t TrackItem) Title() string {
	if t.Track == nil {
		return unknownTitle
	}
	return fmt.Sprintf("%s - %s", t.Track.Title, t.Track.Artist)
}

func (t TrackItem) Description() string {
	if t.Track == nil {
		return ""
	}
	if t.Track.PeerId != "" {
		peer := t.Track.PeerId
		if len(peer) > 8 {
			peer = peer[:8]
		}
		return fmt.Sprintf("%s (%s) · peer %s", t.Track.Album, formatDuration(t.Track.DurationMs), peer)
	}
	return fmt.Sprintf("%s (%s)", t.Track.Album, formatDuration(t.Track.DurationMs))
}

func (t TrackItem) FilterValue() string {
	if t.Track == nil {
		return ""
	}
	return fmt.Sprintf("%s %s %s", t.Track.Title, t.Track.Artist, t.Track.Album)
}

type QueueItem struct {
	Track     *pb.Track
	Index     int
	IsCurrent bool
}

func (q QueueItem) Title() string {
	if q.Track == nil {
		return unknownTitle
	}
	prefix := ""
	if q.IsCurrent {
		prefix = "▶ "
	}
	return prefix + fmt.Sprintf("%d. %s - %s", q.Index+1, q.Track.Title, q.Track.Artist)
}

func (q QueueItem) Description() string {
	if q.Track == nil {
		return ""
	}
	return q.Track.Album
}

func (q QueueItem) FilterValue() string {
	if q.Track == nil {
		return ""
	}
	return fmt.Sprintf("%s %s", q.Track.Title, q.Track.Artist)
}

type PeerItem struct {
	Peer *pb.Peer
}

func (p PeerItem) Title() string {
	if p.Peer == nil {
		return unknownTitle
	}
	id := p.Peer.Id
	if len(id) > 8 {
		id = id[:8]
	}
	dot := "○"
	if p.Peer.Connected {
		dot = "●"
	}
	return fmt.Sprintf("%s Peer %s", dot, id)
}

func (p PeerItem) Description() string {
	if p.Peer == nil {
		return ""
	}

	status := "disconnected"
	if p.Peer.Connected {
		status = "connected"
	}
	if sc := p.Peer.Score; sc != nil {
		latency := "-"
		if sc.AvgLatencyMs > 0 {
			latency = fmt.Sprintf("%.0fms", sc.AvgLatencyMs)
		}

		bandwidth := "-"
		if sc.AvgBandwidth > 0 {
			bandwidth = formatBandwidth(sc.AvgBandwidth) + "/s"
		}
		return fmt.Sprintf("%s | %s | %s | Score: %.1f", status, latency, bandwidth, sc.Score)
	}
	return status
}

func (p PeerItem) FilterValue() string {
	if p.Peer == nil {
		return ""
	}
	return p.Peer.Id
}

// PlaylistItem is a list item wrapping a playlist.
type PlaylistItem struct {
	Playlist *pb.Playlist
}

func (p PlaylistItem) Title() string {
	if p.Playlist == nil {
		return unknownTitle
	}
	return p.Playlist.Name
}

func (p PlaylistItem) Description() string {
	if p.Playlist == nil {
		return ""
	}
	return fmt.Sprintf("%d tracks", len(p.Playlist.TrackIds))
}

func (p PlaylistItem) FilterValue() string {
	if p.Playlist == nil {
		return ""
	}
	return p.Playlist.Name
}

func formatBandwidth(bps int64) string {
	if bps > 1_000_000 {
		return fmt.Sprintf("%.1fMB", float64(bps)/1_000_000)
	}
	if bps > 1_000 {
		return fmt.Sprintf("%.1fKB", float64(bps)/1_000)
	}
	return fmt.Sprintf("%dB", bps)
}

func formatDuration(ms uint64) string {
	totalSec := ms / 1000
	min := totalSec / 60
	sec := totalSec % 60
	return fmt.Sprintf("%d:%02d", min, sec)
}

func formatTimeMs(ms int64) string {
	totalSec := ms / 1000
	min := totalSec / 60
	sec := totalSec % 60
	return fmt.Sprintf("%d:%02d", min, sec)
}

// NewModel constructs the TUI model and initializes the bubbles components.
func NewModel(socketPath string) *Model {
	searchInput := textinput.New()
	searchInput.Placeholder = "Search library..."
	searchInput.Prompt = "/ "
	searchInput.Blur()

	helpViewport := viewport.New(viewport.WithWidth(60), viewport.WithHeight(20))

	m := &Model{
		SocketPath:   socketPath,
		ActivePanel:  PanelLibrary,
		SearchInput:  searchInput,
		HelpViewport: helpViewport,
		Player: PlayerState{
			State:      StateIdle,
			DurationMs: 1,
		},
		Volume: 80,
	}

	m.relayout(80, 24)

	return m
}

func (m *Model) relayout(width, height int) {
	m.Width = width
	m.Height = height

	headerHeight := 3
	footerHeight := 5
	availableHeight := max(height-headerHeight-footerHeight, 1)
	if width >= 120 {
		libWidth := width * 40 / 100
		queueWidth := width * 20 / 100
		peersWidth := width * 20 / 100
		plWidth := width - libWidth - queueWidth - peersWidth
		m.Library = newList(libWidth-2, availableHeight)
		m.Queue = newList(queueWidth-2, availableHeight)
		m.Peers = newList(peersWidth-2, availableHeight)
		m.Playlists = newList(plWidth-2, availableHeight)
	} else {
		m.Library = newList(width-2, availableHeight)
		m.Queue = newList(width-2, availableHeight)
		m.Peers = newList(width-2, availableHeight)
		m.Playlists = newList(width-2, availableHeight)
	}
}

func newList(width, height int) list.Model {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	delegate := list.NewDefaultDelegate()
	l := list.New([]list.Item{}, delegate, width, height)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowTitle(true)
	l.Title = "List"
	return l
}

// Init starts the program: connects to the daemon and starts the event stream.
func (m *Model) Init() tea.Cmd {
	m.IPCClient = ipc.NewClient(m.SocketPath)
	m.EventCh = m.IPCClient.SubscribeWithRetry(
		ipc.EventPlaybackState |
			ipc.EventTrackChanged |
			ipc.EventProgress |
			ipc.EventVolumeChanged |
			ipc.EventQueueUpdated |
			ipc.EventPeerConnected |
			ipc.EventPeerDisconnected |
			ipc.EventLibraryUpdated |
			ipc.EventError,
	)

	go m.eventLoop()
	return tea.Batch(
		m.fetchInitialData(),
		m.fetchLibrary(),
		m.fetchQueue(),
		m.fetchQueueMode(),
		m.fetchPeers(),
		m.fetchPlaylists(),
	)
}

// eventLoop reads events from the subscription and forwards them to the
// Bubble Tea event loop via the stored program reference. It also watches the
// subscription's connection state so the "⟳ Reconnecting…" overlay can fire.
func (m *Model) eventLoop() {
	stateCh := m.EventCh.State()
	for {
		select {
		case event, ok := <-m.EventCh.Events():
			if !ok {
				return
			}

			msg := m.parseEvent(event)
			if msg == nil || m.Program == nil {
				continue
			}
			m.Program.Send(msg)
		case state := <-stateCh:
			if m.Program == nil {
				continue
			}

			switch state {
			case ipc.ConnReconnecting:
				m.Program.Send(ReconnectingMsg{})
			case ipc.ConnConnected:
				m.Program.Send(ConnectedMsg{})
			}
		}
	}
}

// parseEvent decodes an IPC event payload into a Bubble Tea message.
func (m *Model) parseEvent(event *pb.Event) tea.Msg {
	switch event.EventType {
	case pb.EventType_EVENT_TYPE_PLAYBACK_STATE:
		return PlaybackStateMsg{State: string(event.Payload)}
	case pb.EventType_EVENT_TYPE_TRACK_CHANGED:
		var track pb.Track
		if err := proto.Unmarshal(event.Payload, &track); err != nil {
			return nil
		}
		return TrackChangedMsg{Track: &track}
	case pb.EventType_EVENT_TYPE_PROGRESS:
		var progress pb.ProgressEvent
		if err := proto.Unmarshal(event.Payload, &progress); err != nil {
			return nil
		}
		return ProgressMsg{PositionMs: progress.PositionMs, DurationMs: progress.DurationMs}
	case pb.EventType_EVENT_TYPE_VOLUME_CHANGED:
		var vol pb.SetVolumeRequest
		if err := proto.Unmarshal(event.Payload, &vol); err != nil {
			return nil
		}
		return VolumeMsg{Volume: vol.Volume}
	case pb.EventType_EVENT_TYPE_QUEUE_UPDATED:
		var queue pb.QueueResponse
		if err := proto.Unmarshal(event.Payload, &queue); err != nil {
			return nil
		}
		return QueueUpdatedMsg{Tracks: queue.Tracks}
	case pb.EventType_EVENT_TYPE_PEER_CONNECTED:
		var peer pb.Peer
		if err := proto.Unmarshal(event.Payload, &peer); err != nil {
			return nil
		}
		return PeerConnectedMsg{Peer: &peer}
	case pb.EventType_EVENT_TYPE_PEER_SCORE_UPDATED:
		var peer pb.Peer
		if err := proto.Unmarshal(event.Payload, &peer); err != nil {
			return nil
		}
		return PeerScoreUpdatedMsg{Peer: &peer}
	case pb.EventType_EVENT_TYPE_PEER_DISCONNECTED:
		return PeerDisconnectedMsg{PeerID: string(event.Payload)}
	case pb.EventType_EVENT_TYPE_LIBRARY_UPDATED:
		return LibraryUpdatedMsg{}
	case pb.EventType_EVENT_TYPE_ERROR:
		return ErrorMsg{Err: string(event.Payload)}
	default:
		return nil
	}
}

type (
	PlaybackStateMsg struct{ State string }
	TrackChangedMsg  struct{ Track *pb.Track }
	ProgressMsg      struct {
		PositionMs int64
		DurationMs int64
	}
)

type (
	VolumeMsg           struct{ Volume int32 }
	QueueUpdatedMsg     struct{ Tracks []*pb.Track }
	PeerConnectedMsg    struct{ Peer *pb.Peer }
	PeerDisconnectedMsg struct{ PeerID string }
	PeerScoreUpdatedMsg struct{ Peer *pb.Peer }
	LibraryUpdatedMsg   struct{}
	ErrorMsg            struct{ Err string }
	ConnectedMsg        struct{}
	ReconnectingMsg     struct{}
	StatusMsg           struct {
		Err    error
		Status *pb.StatusResponse
	}
)

type LibraryMsg struct {
	Err    error
	Tracks []*pb.Track
}

type QueueMsg struct {
	Err    error
	Tracks []*pb.Track
}

type QueueModeMsg struct {
	Err     error
	Shuffle bool
	Repeat  string
}

type PeersMsg struct {
	Err   error
	Peers []*pb.Peer
}

type PlaylistsMsg struct {
	Err       error
	Playlists []*pb.Playlist
}

type PlaylistTracksMsg struct {
	Err      error
	Playlist *pb.Playlist
	Tracks   []*pb.Track
}

// Update handles all messages and updates the model.
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
		_ = m.EventCh.Close()
	}
	if m.IPCClient != nil {
		_ = m.IPCClient.Close()
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

// fetchInitialData pulls the playback status from the daemon.
func (m *Model) fetchInitialData() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.Status()
		if err != nil {
			return StatusMsg{Err: err}
		}
		if !resp.Success {
			return StatusMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		if eqResp, err := m.IPCClient.GetEqualizer(); err == nil && eqResp.Success {
			if eq := eqResp.GetEqualizer(); eq != nil {
				m.EQ = domain.EqualizerSettings{
					Enabled: eq.Enabled,
					Bass:    float64(eq.BassDb),
					Mid:     float64(eq.MidDb),
					Treble:  float64(eq.TrebleDb),
				}
			}
		}
		return StatusMsg{Status: resp.GetStatus()}
	}
}

// fetchLibrary pulls the full track list from the daemon.
func (m *Model) fetchLibrary() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.ListTracks(0, 0)
		if err != nil {
			return LibraryMsg{Err: err}
		}
		if !resp.Success {
			return LibraryMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		return LibraryMsg{Tracks: resp.GetListTracks().Tracks}
	}
}

// fetchQueue pulls the current playback queue from the daemon.
func (m *Model) fetchQueue() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.GetQueue()
		if err != nil {
			return QueueMsg{Err: err}
		}
		if !resp.Success {
			return QueueMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		return QueueMsg{Tracks: resp.GetQueueList().Tracks}
	}
}

// fetchQueueMode pulls the current shuffle/repeat state from the daemon.
func (m *Model) fetchQueueMode() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.QueueGetMode()
		if err != nil {
			return QueueModeMsg{Err: err}
		}
		if !resp.Success {
			return QueueModeMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		if qm := resp.GetQueueMode(); qm != nil {
			return QueueModeMsg{Shuffle: qm.Shuffle, Repeat: qm.Repeat}
		}
		return QueueModeMsg{}
	}
}

// fetchPeers pulls the discovered peers from the daemon.
func (m *Model) fetchPeers() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.ListPeers()
		if err != nil {
			return PeersMsg{Err: err}
		}
		if !resp.Success {
			return PeersMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		return PeersMsg{Peers: resp.GetListPeers().Peers}
	}
}

func (m *Model) cmdPause() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.Pause()
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

// fetchPlaylists pulls the playlists from the daemon.
func (m *Model) fetchPlaylists() tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.ListPlaylists()
		if err != nil {
			return PlaylistsMsg{Err: err}
		}
		if !resp.Success {
			return PlaylistsMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		if lp := resp.GetListPlaylists(); lp != nil {
			return PlaylistsMsg{Playlists: lp.Playlists}
		}
		return PlaylistsMsg{}
	}
}

// cmdShowSelectedPlaylist loads the tracks of the selected playlist into the
// library panel so they can be played.
func (m *Model) cmdShowSelectedPlaylist() tea.Cmd {
	return func() tea.Msg {
		idx := m.Playlists.Index()
		items := m.Playlists.Items()
		if idx < 0 || idx >= len(items) {
			return nil
		}

		item, ok := items[idx].(PlaylistItem)
		if !ok || item.Playlist == nil {
			return nil
		}

		resp, err := m.IPCClient.GetPlaylist(item.Playlist.Id)
		if err != nil {
			return PlaylistTracksMsg{Err: err}
		}
		if !resp.Success {
			return PlaylistTracksMsg{Err: fmt.Errorf("%s", resp.Error)}
		}

		gp := resp.GetGetPlaylist()
		if gp == nil || gp.Playlist == nil {
			return PlaylistTracksMsg{Err: fmt.Errorf("playlist not found")}
		}
		return PlaylistTracksMsg{Playlist: gp.Playlist, Tracks: gp.Tracks}
	}
}

func (m *Model) cmdResume() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.Resume()
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdNext() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.Next()
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdPrev() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.Prev()
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdStop() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.Stop()
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdSeek(deltaMs int64) tea.Cmd {
	return func() tea.Msg {
		newPos := min(max(m.Player.PositionMs+deltaMs, 0), m.Player.DurationMs)
		err := m.IPCClient.SeekTo(newPos)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdVolume(delta int32) tea.Cmd {
	return func() tea.Msg {
		newVol := min(max(m.Volume+delta, 0), 100)
		_, err := m.IPCClient.SetVolume(newVol)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdSearch(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.SearchRemote(query, 200)
		if err != nil {
			return LibraryMsg{Err: err}
		}
		if !resp.Success {
			return LibraryMsg{Err: fmt.Errorf("%s", resp.Error)}
		}
		return LibraryMsg{Tracks: resp.GetSearch().Tracks}
	}
}

func (m *Model) cmdPlaySelected() tea.Cmd {
	return func() tea.Msg {
		var trackID string
		activeList := m.activeList()
		if activeList != nil {
			idx := activeList.Index()
			items := activeList.Items()
			if idx >= 0 && idx < len(items) {
				switch item := items[idx].(type) {
				case TrackItem:
					if item.Track != nil {
						trackID = item.Track.Id
					}
				case QueueItem:
					// Playing a queued track: Play(trackId) already makes it the
					// queue-current item
					if item.Track != nil {
						trackID = item.Track.Id
					}
				}
			}
		}
		if trackID != "" {
			_, err := m.IPCClient.Play(trackID, "")
			if err != nil {
				return ErrorMsg{Err: err.Error()}
			}
		}
		return nil
	}
}

func (m *Model) cmdQueueRemove() tea.Cmd {
	return func() tea.Msg {
		activeList := m.activeList()
		if activeList != nil {
			idx := activeList.Index()
			if idx >= 0 {
				_, err := m.IPCClient.QueueRemove(int32(idx))
				if err != nil {
					return ErrorMsg{Err: err.Error()}
				}
			}
		}
		return nil
	}
}

func (m *Model) cmdQueueShuffle() tea.Cmd {
	return func() tea.Msg {
		_, err := m.IPCClient.QueueSetShuffle(!m.Player.Shuffle)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		m.Player.Shuffle = !m.Player.Shuffle
		return nil
	}
}

func (m *Model) cmdQueueRepeat() tea.Cmd {
	return func() tea.Msg {
		next := repeatAll
		switch m.Player.RepeatMode {
		case "":
			next = repeatAll
		case repeatAll:
			next = repeatOne
		case repeatOne:
			next = repeatNone
		}
		_, err := m.IPCClient.QueueSetRepeat(next)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		m.Player.RepeatMode = next
		return nil
	}
}

// cmdCycleEQ cycles to the next EQ preset and applies it via IPC.
func (m *Model) cmdCycleEQ() tea.Cmd {
	return func() tea.Msg {
		idx := 0
		for i, p := range domain.EQPresets {
			if p == m.EQ {
				idx = (i + 1) % len(domain.EQPresets)
				break
			}
		}

		next := domain.EQPresets[idx]
		_, err := m.IPCClient.SetEqualizer(next.Enabled, next.Bass, next.Mid, next.Treble)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}

		m.EQ = next
		m.ShowEq = true
		return nil
	}
}

// View renders the entire TUI.
func (m *Model) View() tea.View {
	content := m.renderContent()
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *Model) renderContent() string {
	if m.Err != nil && !m.Connected {
		return m.renderErrorScreen()
	}

	header := m.renderHeader()
	footer := m.renderFooter()

	var searchBar string
	if m.InSearch {
		searchBar = searchStyle.Render(m.SearchInput.View())
	}

	var lists string
	if m.Width >= 120 {
		lists = m.renderWide()
	} else {
		lists = m.renderNarrow()
	}

	if m.ShowHelp {
		help := m.renderHelpOverlay()
		lists = lipgloss.JoinVertical(
			lipgloss.Left,
			lists,
			help,
		)
	}
	if m.ShowLyrics {
		if lyrics := m.renderLyrics(); lyrics != "" {
			lists = lipgloss.JoinVertical(
				lipgloss.Left,
				lists,
				lyrics,
			)
		}
	}
	if m.ShowEq {
		if eq := m.renderEQ(); eq != "" {
			lists = lipgloss.JoinVertical(
				lipgloss.Left,
				lists,
				eq,
			)
		}
	}
	if m.Reconnecting {
		overlay := reconnectStyle.Render(" ⟳ Reconnecting... ")
		lists = lipgloss.JoinVertical(
			lipgloss.Left,
			lists,
			overlay,
		)
	}

	parts := []string{header}
	if searchBar != "" {
		parts = append(parts, searchBar)
	}

	parts = append(parts, lists, footer)
	return lipgloss.JoinVertical(
		lipgloss.Left,
		parts...,
	)
}
