package tui

import (
	"fmt"

	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"google.golang.org/protobuf/proto"

	"github.com/p-society/raag/internal/domain"
	ipcclient "github.com/p-society/raag/internal/infra/ipc/client"
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
	IPCClient  *ipcclient.Client
	EventCh    *ipcclient.EventClient
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
	m.IPCClient = ipcclient.NewClient(m.SocketPath)
	m.EventCh = m.IPCClient.SubscribeWithRetry(
		ipcclient.EventPlaybackState |
			ipcclient.EventTrackChanged |
			ipcclient.EventProgress |
			ipcclient.EventVolumeChanged |
			ipcclient.EventQueueUpdated |
			ipcclient.EventPeerConnected |
			ipcclient.EventPeerDisconnected |
			ipcclient.EventLibraryUpdated |
			ipcclient.EventError,
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
			case ipcclient.ConnReconnecting:
				m.Program.Send(ReconnectingMsg{})
			case ipcclient.ConnConnected:
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
