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

	"github.com/p-society/raag/internal/infra/ipc"
	pb "github.com/p-society/raag/proto/gen"
)

type Panel int

const (
	PanelLibrary Panel = iota
	PanelQueue
	PanelPeers
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

// Model is the root Bubble Tea model for the Raag TUI.
type Model struct {
	SocketPath string
	IPCClient  *ipc.Client
	EventCh    *ipc.EventClient
	Program    *tea.Program

	Width  int
	Height int

	ActivePanel Panel

	Library list.Model
	Queue   list.Model
	Peers   list.Model

	SearchInput textinput.Model
	InSearch    bool

	HelpViewport viewport.Model
	ShowHelp     bool

	Player PlayerState

	Err error

	Connected    bool
	Reconnecting bool
	PeerCount    int
	Volume       int32

	Scanning bool
}

type PlayerState struct {
	State        PlaybackState
	CurrentTrack *pb.Track
	PositionMs   int64
	DurationMs   int64
	QueueLen     int32
	QueuePos     int32
	Shuffle      bool
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
	return fmt.Sprintf("Peer %s", id)
}

func (p PeerItem) Description() string {
	if p.Peer == nil {
		return ""
	}
	status := "disconnected"
	if p.Peer.Connected {
		status = "connected"
	}
	if p.Peer.Score != nil {
		return fmt.Sprintf("%s | Score: %.1f | BW: %s/s", status, p.Peer.Score.Score, formatBandwidth(p.Peer.Score.AvgBandwidth))
	}
	return status
}

func (p PeerItem) FilterValue() string {
	if p.Peer == nil {
		return ""
	}
	return p.Peer.Id
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
		libWidth := width * 50 / 100
		queueWidth := width * 30 / 100
		peersWidth := width - libWidth - queueWidth
		m.Library = newList(libWidth-2, availableHeight)
		m.Queue = newList(queueWidth-2, availableHeight)
		m.Peers = newList(peersWidth-2, availableHeight)
	} else {
		m.Library = newList(width-2, availableHeight)
		m.Queue = newList(width-2, availableHeight)
		m.Peers = newList(width-2, availableHeight)
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
		m.fetchPeers(),
	)
}

// eventLoop reads events from the subscription and forwards them to the
// Bubble Tea event loop via the stored program reference.
func (m *Model) eventLoop() {
	for event := range m.EventCh.Events() {
		msg := m.parseEvent(event)
		if msg == nil {
			continue
		}
		if m.Program != nil {
			m.Program.Send(msg)
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
type PeersMsg struct {
	Err   error
	Peers []*pb.Peer
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

	case PeersMsg:
		cmds = append(cmds, m.handlePeersMsg(msg)...)
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

func (m *Model) handlePeerDisconnected(msg PeerDisconnectedMsg) []tea.Cmd {
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
		m.fetchPeers(),
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
		switch msg.String() {
		case "esc":
			m.InSearch = false
			m.SearchInput.Blur()
		case "enter":
			m.InSearch = false
			m.SearchInput.Blur()
			query := m.SearchInput.Value()
			if query != "" {
				cmds = append(cmds, m.cmdSearch(query))
			}
		}
		return cmds
	}

	if m.ShowHelp {
		if key.Matches(msg, keys.Help) {
			m.ShowHelp = false
		}
		return cmds
	}

	switch {
	case key.Matches(msg, keys.Quit):
		if m.EventCh != nil {
			_ = m.EventCh.Close()
		}
		return []tea.Cmd{tea.Quit}
	case key.Matches(msg, keys.Help):
		m.ShowHelp = !m.ShowHelp
	case key.Matches(msg, keys.Tab):
		m.ActivePanel = (m.ActivePanel + 1) % 3
	case key.Matches(msg, keys.Search):
		m.InSearch = true
		m.SearchInput.Focus()
	case key.Matches(msg, keys.PlayPause):
		if m.Player.State == StatePlaying {
			cmds = append(cmds, m.cmdPause())
		} else {
			cmds = append(cmds, m.cmdResume())
		}
	case key.Matches(msg, keys.Next):
		cmds = append(cmds, m.cmdNext())
	case key.Matches(msg, keys.Prev):
		cmds = append(cmds, m.cmdPrev())
	case key.Matches(msg, keys.Stop):
		cmds = append(cmds, m.cmdStop())
	case key.Matches(msg, keys.SeekFwd):
		cmds = append(cmds, m.cmdSeek(10000))
	case key.Matches(msg, keys.SeekBack):
		cmds = append(cmds, m.cmdSeek(-10000))
	case key.Matches(msg, keys.VolUp):
		cmds = append(cmds, m.cmdVolume(5))
	case key.Matches(msg, keys.VolDown):
		cmds = append(cmds, m.cmdVolume(-5))
	case key.Matches(msg, keys.Enter):
		cmds = append(cmds, m.cmdPlaySelected())
	case key.Matches(msg, keys.Delete):
		if m.ActivePanel == PanelQueue {
			cmds = append(cmds, m.cmdQueueRemove())
		}
	case key.Matches(msg, keys.Refresh):
		cmds = append(cmds, m.fetchLibrary(), m.fetchQueue(), m.fetchPeers())
	}

	return cmds
}

func (m *Model) activeList() *list.Model {
	switch m.ActivePanel {
	case PanelLibrary:
		return &m.Library
	case PanelQueue:
		return &m.Queue
	case PanelPeers:
		return &m.Peers
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
		newPos := max(m.Player.PositionMs+deltaMs, 0)
		if newPos > m.Player.DurationMs {
			newPos = m.Player.DurationMs
		}
		err := m.IPCClient.SeekTo(newPos)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdVolume(delta int32) tea.Cmd {
	return func() tea.Msg {
		newVol := max(m.Volume+delta, 0)
		if newVol > 100 {
			newVol = 100
		}
		_, err := m.IPCClient.SetVolume(newVol)
		if err != nil {
			return ErrorMsg{Err: err.Error()}
		}
		return nil
	}
}

func (m *Model) cmdSearch(query string) tea.Cmd {
	return func() tea.Msg {
		resp, err := m.IPCClient.Search(query, 200)
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
				if t, ok := items[idx].(TrackItem); ok && t.Track != nil {
					trackID = t.Track.Id
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

	if m.Reconnecting {
		overlay := reconnectStyle.Render(" ⟳ Reconnecting... ")
		lists = lipgloss.JoinVertical(
			lipgloss.Left,
			lists,
			overlay,
		)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		lists,
		footer,
	)
}
