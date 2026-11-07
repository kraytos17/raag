package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/observability"
	p2pnode "github.com/p-society/raag/internal/infra/p2p/node"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
	"golang.org/x/sync/semaphore"
	"google.golang.org/protobuf/proto"
)

const (
	errP2PNotEnabled        = "P2P is not enabled"
	errPlaylistsUnavailable = "playlists not available"
	errQueueUnavailable     = "queue is not available"

	// unknownCommand is the IPC metrics label for requests with no payload.
	unknownCommand = "unknown"
)

type Server struct {
	socketPath string
	listener   net.Listener

	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	playback     PlaybackHandler
	scanner      ScannerHandler
	search       SearchHandler
	libraryRepo  LibraryRepoHandler
	peerRepo     PeerRepoHandler
	queue        QueueHandler
	playlistRepo app.PlaylistRepository
	p2pNode      *p2pnode.Node
	eventBus     domain.EventBus
	metrics      *observability.Metrics
	dbStats      DebugStatsProvider

	maxConns     *semaphore.Weighted
	progressCb   func(jobID string, scanned int, total int, currentFile string, phase string)
	progressCbMu sync.RWMutex

	subMgr *SubscriptionManager
}
type Config struct {
	Playback     PlaybackHandler
	Scanner      ScannerHandler
	Search       SearchHandler
	LibraryRepo  LibraryRepoHandler
	PeerRepo     PeerRepoHandler
	Queue        QueueHandler
	PlaylistRepo app.PlaylistRepository
	P2PNode      *p2pnode.Node
	EventBus     domain.EventBus
	Metrics      *observability.Metrics
	DBStats      DebugStatsProvider
}

func (c *Config) Validate() error {
	if c.Playback == nil {
		return errors.New("playback handler is required")
	}
	if c.Scanner == nil {
		return errors.New("scanner handler is required")
	}
	if c.Search == nil {
		return errors.New("search handler is required")
	}
	if c.LibraryRepo == nil {
		return errors.New("library repo handler is required")
	}
	if c.Queue == nil {
		return errors.New("queue handler is required")
	}
	return nil
}

func NewServer(socketPath string, config Config) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid server config: %w", err)
	}
	server := &Server{
		socketPath:   socketPath,
		done:         make(chan struct{}),
		playback:     config.Playback,
		scanner:      config.Scanner,
		search:       config.Search,
		libraryRepo:  config.LibraryRepo,
		peerRepo:     config.PeerRepo,
		queue:        config.Queue,
		playlistRepo: config.PlaylistRepo,
		p2pNode:      config.P2PNode,
		eventBus:     config.EventBus,
		metrics:      config.Metrics,
		dbStats:      config.DBStats,
		maxConns:     semaphore.NewWeighted(64),
		subMgr:       NewSubscriptionManager(),
	}

	if config.Playback != nil {
		config.Playback.OnProgress(func(posMs, durMs int64) {
			server.publishPlaybackProgress(posMs, durMs)
		})
	}
	if config.EventBus != nil {
		server.wireEventBus(config.EventBus)
	}
	return server, nil
}

// wireEventBus subscribes the IPC server to the domain event bus so that
// lifecycle events (peer, scan, queue, playback) are forwarded to IPC clients.
func (s *Server) wireEventBus(bus domain.EventBus) {
	for _, t := range []domain.EventType{
		domain.EventTrackStarted,
		domain.EventTrackFinished,
		domain.EventTrackPaused,
		domain.EventTrackResumed,
		domain.EventTrackSeeked,
		domain.EventPeerConnected,
		domain.EventPeerDisconnected,
		domain.EventPeerScoreUpdated,
		domain.EventScanStarted,
		domain.EventScanComplete,
		domain.EventQueueUpdated,
		domain.EventPlaybackBuffering,
		domain.EventPlaybackReady,
		domain.EventVolumeChanged,
	} {
		bus.Subscribe(t, func(e domain.Event) {
			eventType, payload := translateEvent(s, e)
			if eventType == pb.EventType_EVENT_TYPE_UNSPECIFIED {
				return
			}
			s.PublishEvent(uint32(eventType), payload)
		})
	}
}

// translateEvent maps a domain event to an IPC event type and payload.
func translateEvent(s *Server, e domain.Event) (pb.EventType, []byte) {
	switch e.Type {
	case domain.EventTrackStarted:
		return pb.EventType_EVENT_TYPE_TRACK_CHANGED, mustMarshal(convert.TrackToProto(s.playback.GetCurrentTrack()))
	case domain.EventTrackFinished:
		return pb.EventType_EVENT_TYPE_PLAYBACK_STATE, []byte("stopped")
	case domain.EventTrackPaused:
		return pb.EventType_EVENT_TYPE_PLAYBACK_STATE, []byte("paused")
	case domain.EventTrackResumed:
		return pb.EventType_EVENT_TYPE_PLAYBACK_STATE, []byte("playing")
	case domain.EventTrackSeeked:
		return pb.EventType_EVENT_TYPE_TRACK_CHANGED, mustMarshal(convert.TrackToProto(s.playback.GetCurrentTrack()))
	case domain.EventPeerConnected:
		var pid domain.PeerID
		switch p := e.Payload.(type) {
		case domain.PeerConnectedPayload:
			pid = p.PeerID
		case domain.PeerID:
			pid = p
		default:
			return pb.EventType_EVENT_TYPE_UNSPECIFIED, nil
		}

		info, err := s.peerRepo.GetPeerInfo(context.Background(), pid)
		if err != nil {
			return pb.EventType_EVENT_TYPE_PEER_CONNECTED, []byte(pid.String())
		}

		peer := convert.PeerInfoToProto(info)
		return pb.EventType_EVENT_TYPE_PEER_CONNECTED, mustMarshal(peer)
	case domain.EventPeerScoreUpdated:
		// Score refresh is not a new connection: map to its own event type so
		// the TUI doesn't inflate PeerCount.
		var pid domain.PeerID
		switch p := e.Payload.(type) {
		case domain.PeerScoreUpdatedPayload:
			pid = p.PeerID
		default:
			return pb.EventType_EVENT_TYPE_PEER_SCORE_UPDATED, nil
		}

		info, err := s.peerRepo.GetPeerInfo(context.Background(), pid)
		if err != nil {
			return pb.EventType_EVENT_TYPE_PEER_SCORE_UPDATED, []byte(pid.String())
		}

		peer := convert.PeerInfoToProto(info)
		return pb.EventType_EVENT_TYPE_PEER_SCORE_UPDATED, mustMarshal(peer)
	case domain.EventPeerDisconnected:
		var pid domain.PeerID
		switch p := e.Payload.(type) {
		case domain.PeerDisconnectedPayload:
			pid = p.PeerID
		case domain.PeerID:
			pid = p
		default:
			return pb.EventType_EVENT_TYPE_UNSPECIFIED, nil
		}
		return pb.EventType_EVENT_TYPE_PEER_DISCONNECTED, []byte(pid.String())
	case domain.EventScanStarted:
		return pb.EventType_EVENT_TYPE_LIBRARY_UPDATED, []byte("started")
	case domain.EventScanComplete:
		return pb.EventType_EVENT_TYPE_LIBRARY_UPDATED, []byte("complete")
	case domain.EventQueueUpdated:
		tracks := s.queue.Tracks()
		qt := make([]*pb.Track, len(tracks))
		for i, t := range tracks {
			qt[i] = convert.TrackToProto(t)
		}
		return pb.EventType_EVENT_TYPE_QUEUE_UPDATED, mustMarshal(&pb.QueueResponse{Tracks: qt})
	case domain.EventPlaybackBuffering:
		return pb.EventType_EVENT_TYPE_PLAYBACK_STATE, []byte("buffering")
	case domain.EventPlaybackReady:
		return pb.EventType_EVENT_TYPE_PLAYBACK_STATE, []byte("playing")
	case domain.EventVolumeChanged:
		payload, ok := e.Payload.(domain.VolumeChangedPayload)
		if !ok {
			return pb.EventType_EVENT_TYPE_UNSPECIFIED, nil
		}
		return pb.EventType_EVENT_TYPE_VOLUME_CHANGED,
			mustMarshal(&pb.SetVolumeRequest{Volume: int32(payload.Volume)})
	default:
		return pb.EventType_EVENT_TYPE_UNSPECIFIED, nil
	}
}

func mustMarshal(msg proto.Message) []byte {
	if msg == nil {
		return nil
	}

	data, err := proto.Marshal(msg)
	if err != nil {
		return nil
	}
	return data
}

func (s *Server) PublishEvent(eventType uint32, payload []byte) {
	if s.subMgr != nil {
		s.subMgr.Broadcast(eventType, payload)
	}
}

func (s *Server) broadcast(resp *pb.Response) {
	for _, st := range s.subMgr.All() {
		// Scan progress is intentionally not event-mask gated: every
		// connected client receives it.
		if err := s.subMgr.WriteTo(st.conn, resp); err != nil {
			slog.Debug("failed to broadcast to conn", "error", err)
		}
	}
}

// publishPlaybackState broadcasts playback state change
func (s *Server) Start(ctx context.Context) error {
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	lis, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}

	s.listener = lis
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		s.listener.Close()
		return err
	}

	slog.Info("IPC server listening", "path", s.socketPath)
	s.wg.Go(func() { s.acceptLoop(ctx) })
	return nil
}

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			if s.isListenerClosed(err) {
				return
			}
			slog.Warn("accept failed", "error", err)
			continue
		}
		if err := s.maxConns.Acquire(ctx, 1); err != nil {
			conn.Close()
			continue
		}

		s.subMgr.Register(conn)
		s.wg.Go(func() {
			defer s.maxConns.Release(1)
			s.handleConn(ctx, conn)
		})
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer func() {
		conn.Close()
		s.subMgr.Unsubscribe(conn)
	}()

	var eventMask uint32
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		if err := conn.SetReadDeadline(time.Now().Add(domain.IPCReadTimeout)); err != nil {
			slog.Warn("failed to set read deadline", "error", err)
			return
		}

		var req pb.Request
		if err := wire.ReadMsg(conn, &req); err != nil {
			if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
				slog.Debug("ipc: connection idle timeout", "remote", conn.RemoteAddr())
				return
			}
			if !isConnClosed(err) {
				slog.Debug("ipc: read error", "error", err)
			}
			return
		}
		if sub, ok := req.Payload.(*pb.Request_Subscribe); ok {
			eventMask = sub.Subscribe.EventMask
			s.subMgr.Subscribe(conn, eventMask)
			slog.Debug("client subscribed to events", "mask", eventMask)
		}

		start := time.Now()
		resp := s.dispatch(ctx, &req)
		if err := conn.SetWriteDeadline(time.Now().Add(domain.IPCWriteTimeout)); err != nil {
			slog.Warn("failed to set write deadline", "error", err)
			return
		}
		if err := s.subMgr.WriteTo(conn, resp); err != nil {
			slog.Warn("write response failed", "error", err)
			return
		}
		if s.metrics != nil && s.metrics.IPCDuration != nil {
			s.metrics.IPCDuration.WithLabelValues(ipcCommandLabel(&req)).Observe(time.Since(start).Seconds())
		}
	}
}

// ipcCommandLabel returns a stable label for an IPC request based on its
// payload message name (e.g. "Request_Play"), or unknownCommand if the payload
// is nil. The oneof wrapper types do not implement proto.Message, so the label
// is derived via reflection on the payload's concrete type instead.
func ipcCommandLabel(req *pb.Request) string {
	if req == nil || req.Payload == nil {
		return unknownCommand
	}

	t := reflect.TypeOf(req.Payload)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() != "" {
		return t.Name()
	}
	return unknownCommand
}

// dispatchTable routes each request payload type to its handler. The oneof
// wrapper types do not implement proto.Message, so the map is keyed by
// reflection on the payload's concrete type (mirroring ipcCommandLabel).
// Adding a request type means adding one row here plus its handler file.
var dispatchTable = map[reflect.Type]func(s *Server, ctx context.Context, req *pb.Request) *pb.Response{
	reflect.TypeFor[*pb.Request_Play](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handlePlay(ctx, req.Payload.(*pb.Request_Play).Play)
	},
	reflect.TypeFor[*pb.Request_Pause](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handlePause(ctx)
	},
	reflect.TypeFor[*pb.Request_Resume](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleResume(ctx)
	},
	reflect.TypeFor[*pb.Request_Stop](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleStop(ctx)
	},
	reflect.TypeFor[*pb.Request_Next](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleNext(ctx)
	},
	reflect.TypeFor[*pb.Request_Prev](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handlePrev(ctx)
	},
	reflect.TypeFor[*pb.Request_Seek](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleSeek(ctx, req.Payload.(*pb.Request_Seek).Seek)
	},
	reflect.TypeFor[*pb.Request_SetVolume](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleSetVolume(ctx, req.Payload.(*pb.Request_SetVolume).SetVolume)
	},
	reflect.TypeFor[*pb.Request_QueueAdd](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleQueueAdd(ctx, req.Payload.(*pb.Request_QueueAdd).QueueAdd)
	},
	reflect.TypeFor[*pb.Request_QueueRemove](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleQueueRemove(req.Payload.(*pb.Request_QueueRemove).QueueRemove)
	},
	reflect.TypeFor[*pb.Request_QueueClear](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleQueueClear()
	},
	reflect.TypeFor[*pb.Request_Search](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleSearch(ctx, req.Payload.(*pb.Request_Search).Search)
	},
	reflect.TypeFor[*pb.Request_LibScan](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleLibScanAsync(req.Payload.(*pb.Request_LibScan).LibScan)
	},
	reflect.TypeFor[*pb.Request_ListPeers](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleListPeers(ctx)
	},
	reflect.TypeFor[*pb.Request_Status](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleStatus()
	},
	reflect.TypeFor[*pb.Request_HealthCheck](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleHealthCheck()
	},
	reflect.TypeFor[*pb.Request_CreatePlaylist](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleCreatePlaylist(ctx, req.Payload.(*pb.Request_CreatePlaylist).CreatePlaylist)
	},
	reflect.TypeFor[*pb.Request_GetPlaylist](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleGetPlaylist(ctx, req.Payload.(*pb.Request_GetPlaylist).GetPlaylist)
	},
	reflect.TypeFor[*pb.Request_ListPlaylists](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleListPlaylists(ctx)
	},
	reflect.TypeFor[*pb.Request_AddToPlaylist](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleAddToPlaylist(ctx, req.Payload.(*pb.Request_AddToPlaylist).AddToPlaylist)
	},
	reflect.TypeFor[*pb.Request_GetTrack](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleGetTrack(ctx, req.Payload.(*pb.Request_GetTrack).GetTrack)
	},
	reflect.TypeFor[*pb.Request_GetTrackByPath](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleGetTrackByPath(ctx, req.Payload.(*pb.Request_GetTrackByPath).GetTrackByPath)
	},
	reflect.TypeFor[*pb.Request_NetworkStatus](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleNetworkStatus()
	},
	reflect.TypeFor[*pb.Request_BanPeer](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleBanPeer(req.Payload.(*pb.Request_BanPeer).BanPeer)
	},
	reflect.TypeFor[*pb.Request_UnbanPeer](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleUnbanPeer(req.Payload.(*pb.Request_UnbanPeer).UnbanPeer)
	},
	reflect.TypeFor[*pb.Request_ListTracks](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleListTracks(ctx, req.Payload.(*pb.Request_ListTracks).ListTracks)
	},
	reflect.TypeFor[*pb.Request_QueueList](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleQueueList()
	},
	reflect.TypeFor[*pb.Request_QueueShuffle](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleQueueShuffle(req.Payload.(*pb.Request_QueueShuffle).QueueShuffle)
	},
	reflect.TypeFor[*pb.Request_QueueRepeat](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleQueueRepeat(req.Payload.(*pb.Request_QueueRepeat).QueueRepeat)
	},
	reflect.TypeFor[*pb.Request_QueueMode](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleQueueMode()
	},
	reflect.TypeFor[*pb.Request_Equalizer](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleSetEqualizer(req.Payload.(*pb.Request_Equalizer).Equalizer)
	},
	reflect.TypeFor[*pb.Request_DebugStats](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return s.handleDebugStats()
	},
	reflect.TypeFor[*pb.Request_Subscribe](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		// The conn was already registered with the SubscriptionManager in
		// handleConn before dispatch; just acknowledge so the client doesn't
		// get a bogus "unknown request type" error.
		return &pb.Response{Success: true}
	},
	reflect.TypeFor[*pb.Request_Unsubscribe](): func(s *Server, ctx context.Context, req *pb.Request) *pb.Response {
		return &pb.Response{Success: true}
	},
}

func (s *Server) dispatch(ctx context.Context, req *pb.Request) *pb.Response {
	var resp *pb.Response
	if req.Payload != nil {
		if handle, ok := dispatchTable[reflect.TypeOf(req.Payload)]; ok {
			resp = handle(s, ctx, req)
		}
	}
	if resp == nil {
		resp = &pb.Response{Success: false, Error: "unknown request type"}
	}

	resp.RequestId = req.RequestId
	return resp
}

func (s *Server) Stop(ctx context.Context) error {
	s.closeOnce.Do(func() {
		close(s.done)
	})

	if s.listener != nil {
		s.listener.Close()
	}
	for _, st := range s.subMgr.All() {
		st.conn.Close()
	}

	s.wg.Wait()
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("failed to remove socket file", "path", s.socketPath, "error", err)
	}
	return nil
}

func (s *Server) isListenerClosed(err error) bool {
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	if opErr, ok := errors.AsType[*net.OpError](err); ok {
		return opErr.Op == "accept"
	}
	return false
}

func isConnClosed(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "use of closed network connection") ||
		errors.Is(err, net.ErrClosed)
}

type ServerComponent struct {
	*Server
}

func (c *ServerComponent) Name() string {
	return "ipc-server"
}

func AsComponent(server *Server) app.Component {
	return &ServerComponent{Server: server}
}
