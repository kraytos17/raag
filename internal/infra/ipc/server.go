package ipc

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/observability"
	p2p "github.com/p-society/raag/internal/infra/p2p"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
	"golang.org/x/sync/semaphore"
	"google.golang.org/protobuf/proto"
)

const (
	errP2PNotEnabled        = "P2P is not enabled"
	errPlaylistsUnavailable = "playlists not available"
	errQueueUnavailable     = "queue is not available"
)

type Server struct {
	socketPath string
	listener   net.Listener

	mu    sync.Mutex
	conns []net.Conn
	// connWriteMu serializes writes to each conn so broadcast (scan progress)
	// and handleConn response writes can't interleave frames.
	connWriteMu map[net.Conn]*sync.Mutex

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
	p2pNode      *p2p.P2PNode
	eventBus     domain.EventBus
	metrics      *observability.Metrics

	maxConns     *semaphore.Weighted
	progressCb   func(jobID string, scanned int, total int, currentFile string, phase string)
	progressCbMu sync.RWMutex

	subMgr *SubscriptionManager
}

type ServerConfig struct {
	Playback     PlaybackHandler
	Scanner      ScannerHandler
	Search       SearchHandler
	LibraryRepo  LibraryRepoHandler
	PeerRepo     PeerRepoHandler
	Queue        QueueHandler
	PlaylistRepo app.PlaylistRepository
	P2PNode      *p2p.P2PNode
	EventBus     domain.EventBus
	Metrics      *observability.Metrics
}

func (c *ServerConfig) Validate() error {
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

type PlaybackHandler interface {
	Play(ctx context.Context, trackID domain.TrackID) error
	PlayQuery(ctx context.Context, query string) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Stop(ctx context.Context) error
	Seek(ctx context.Context, dur time.Duration) error
	SetVolume(ctx context.Context, vol int) error
	GetState() domain.PlayerState
	GetVolume() int
	GetCurrentTrack() *domain.Track
	GetPosition() time.Duration
	OnProgress(callback func(positionMs, durationMs int64))
}

type ScannerHandler interface {
	Scan(ctx context.Context) (int, error)
	ScanIncremental(ctx context.Context) (added, modified, removed int, err error)
	OnProgress(fn func(app.ScanProgress))
	RemoveProgressHandler(fn func(app.ScanProgress))
}

type SearchHandler interface {
	Search(ctx context.Context, query string, limit int) ([]*domain.Track, error)
}

type LibraryRepoHandler interface {
	FindByID(ctx context.Context, trackID domain.TrackID) (*domain.Track, error)
	FindByPath(ctx context.Context, path string) (*domain.Track, error)
	ListAll(ctx context.Context) ([]*domain.Track, error)
}

type PeerRepoHandler interface {
	ListAll(ctx context.Context) iter.Seq2[*domain.PeerInfo, error]
	GetPeerInfo(ctx context.Context, id domain.PeerID) (*domain.PeerInfo, error)
}

type QueueHandler interface {
	Add(track *domain.Track)
	Insert(pos int, track *domain.Track)
	Remove(pos int)
	Clear()
	Length() int
	Position() int
	MoveTo(track *domain.Track) bool
	Next() *domain.Track
	Previous() *domain.Track
	Tracks() []*domain.Track
	ToggleShuffle()
	SetShuffle(shuffle bool)
	SetRepeat(mode domain.RepeatMode)
	GetShuffle() bool
	GetRepeat() domain.RepeatMode
}

func NewServer(socketPath string, config ServerConfig) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid server config: %w", err)
	}
	server := &Server{
		socketPath:   socketPath,
		done:         make(chan struct{}),
		connWriteMu:  make(map[net.Conn]*sync.Mutex),
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
			return pb.EventType_EVENT_TYPE_PEER_CONNECTED, []byte(string(pid))
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
			return pb.EventType_EVENT_TYPE_PEER_SCORE_UPDATED, []byte(string(pid))
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
		return pb.EventType_EVENT_TYPE_PEER_DISCONNECTED, []byte(string(pid))
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

func (s *Server) SetProgressCallback(fn func(jobID string, scanned int, total int, currentFile string, phase string)) {
	s.progressCbMu.Lock()
	defer s.progressCbMu.Unlock()
	s.progressCb = fn
}

// PublishEvent broadcasts an event to all subscribed clients
func (s *Server) PublishEvent(eventType uint32, payload []byte) {
	if s.subMgr != nil {
		s.subMgr.Broadcast(eventType, payload)
	}
}

func (s *Server) publishProgress(jobID string, scanned int, total int, currentFile string, phase string) {
	s.progressCbMu.RLock()
	cb := s.progressCb
	s.progressCbMu.RUnlock()
	if cb != nil {
		cb(jobID, scanned, total, currentFile, phase)
	}

	progress := &pb.ScanProgress{
		JobId:       jobID,
		Scanned:     int32(scanned),
		Total:       int32(total),
		CurrentFile: currentFile,
		Phase:       phase,
	}
	resp := &pb.Response{
		Success: true,
		JobId:   jobID,
		Payload: &pb.Response_ScanProgress{ScanProgress: progress},
	}
	s.broadcast(resp)
}

func (s *Server) broadcast(resp *pb.Response) {
	s.mu.Lock()
	conns := make([]net.Conn, len(s.conns))
	copy(conns, s.conns)
	s.mu.Unlock()

	for _, conn := range conns {
		if err := s.writeToConn(conn, resp); err != nil {
			slog.Debug("failed to broadcast to conn", "error", err)
		}
	}
}

// writeToConn serializes a response write to a conn with its per-conn mutex so
// concurrent writes (broadcast vs handleConn response) can't interleave frames.
func (s *Server) writeToConn(conn net.Conn, msg *pb.Response) error {
	s.mu.Lock()
	mu := s.connWriteMu[conn]
	s.mu.Unlock()

	if mu == nil {
		mu = &sync.Mutex{}
	}
	mu.Lock()
	defer mu.Unlock()
	return wire.WriteMsg(conn, msg)
}

// publishPlaybackState broadcasts playback state change
func (s *Server) publishPlaybackState(state string) {
	s.PublishEvent(EventPlaybackState, []byte(state))
}

// publishTrackChanged broadcasts track change
func (s *Server) publishTrackChanged(track *domain.Track) {
	pbTrack := convert.TrackToProto(track)
	data, _ := proto.Marshal(pbTrack)
	s.PublishEvent(EventTrackChanged, data)
}

// publishPlaybackProgress broadcasts playback progress (lightweight)
func (s *Server) publishPlaybackProgress(posMs, durMs int64) {
	progress := &pb.ProgressEvent{
		PositionMs: posMs,
		DurationMs: durMs,
	}
	data, _ := proto.Marshal(progress)
	s.PublishEvent(EventProgress, data)
}

// publishVolumeChange broadcasts volume change
func (s *Server) publishVolumeChange(volume int32) {
	data, _ := proto.Marshal(&pb.SetVolumeRequest{Volume: volume})
	s.PublishEvent(EventVolumeChanged, data)
}

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
		_ = s.listener.Close()
		return err
	}

	slog.Info("IPC server listening", "path", s.socketPath)
	s.wg.Add(1)
	go s.acceptLoop(ctx)
	return nil
}

func (s *Server) acceptLoop(ctx context.Context) {
	defer s.wg.Done()

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
			_ = conn.Close()
			continue
		}

		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.connWriteMu[conn] = &sync.Mutex{}
		s.mu.Unlock()

		s.wg.Add(1)
		go func() {
			defer s.maxConns.Release(1)
			s.handleConn(ctx, conn)
		}()
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		_ = conn.Close()
		s.removeConn(conn)
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
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
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
		if err := s.writeToConn(conn, resp); err != nil {
			slog.Warn("write response failed", "error", err)
			return
		}
		if s.metrics != nil && s.metrics.IPCDuration != nil {
			s.metrics.IPCDuration.WithLabelValues(ipcCommandLabel(&req)).Observe(time.Since(start).Seconds())
		}
	}
}

// ipcCommandLabel returns a stable label for an IPC request based on its
// payload message name (e.g. "Request_Play"), or "unknown" if the payload is nil.
func ipcCommandLabel(req *pb.Request) string {
	if req == nil || req.Payload == nil {
		return "unknown"
	}
	return string(proto.MessageName(req.Payload.(proto.Message)))
}

//nolint:gocyclo // dispatch function has many cases for IPC requests
func (s *Server) dispatch(ctx context.Context, req *pb.Request) *pb.Response {
	var resp *pb.Response
	switch p := req.Payload.(type) {
	case *pb.Request_Play:
		resp = s.handlePlay(ctx, p.Play)
	case *pb.Request_Pause:
		resp = s.handlePause(ctx)
	case *pb.Request_Resume:
		resp = s.handleResume(ctx)
	case *pb.Request_Stop:
		resp = s.handleStop(ctx)
	case *pb.Request_Next:
		resp = s.handleNext(ctx)
	case *pb.Request_Prev:
		resp = s.handlePrev(ctx)
	case *pb.Request_Seek:
		resp = s.handleSeek(ctx, p.Seek)
	case *pb.Request_SetVolume:
		resp = s.handleSetVolume(ctx, p.SetVolume)
	case *pb.Request_QueueAdd:
		resp = s.handleQueueAdd(ctx, p.QueueAdd)
	case *pb.Request_QueueRemove:
		resp = s.handleQueueRemove(p.QueueRemove)
	case *pb.Request_QueueClear:
		resp = s.handleQueueClear()
	case *pb.Request_Search:
		resp = s.handleSearch(ctx, p.Search)
	case *pb.Request_LibScan:
		resp = s.handleLibScanAsync(p.LibScan)
	case *pb.Request_ListPeers:
		resp = s.handleListPeers(ctx)
	case *pb.Request_Status:
		resp = s.handleStatus()
	case *pb.Request_HealthCheck:
		resp = s.handleHealthCheck()
	case *pb.Request_CreatePlaylist:
		resp = s.handleCreatePlaylist(ctx, p.CreatePlaylist)
	case *pb.Request_GetPlaylist:
		resp = s.handleGetPlaylist(ctx, p.GetPlaylist)
	case *pb.Request_ListPlaylists:
		resp = s.handleListPlaylists(ctx)
	case *pb.Request_AddToPlaylist:
		resp = s.handleAddToPlaylist(ctx, p.AddToPlaylist)
	case *pb.Request_GetTrack:
		resp = s.handleGetTrack(ctx, p.GetTrack)
	case *pb.Request_GetTrackByPath:
		resp = s.handleGetTrackByPath(ctx, p.GetTrackByPath)
	case *pb.Request_NetworkStatus:
		resp = s.handleNetworkStatus()
	case *pb.Request_BanPeer:
		resp = s.handleBanPeer(p.BanPeer)
	case *pb.Request_UnbanPeer:
		resp = s.handleUnbanPeer(p.UnbanPeer)
	case *pb.Request_ListTracks:
		resp = s.handleListTracks(ctx, p.ListTracks)
	case *pb.Request_QueueList:
		resp = s.handleQueueList()
	case *pb.Request_QueueShuffle:
		resp = s.handleQueueShuffle(p.QueueShuffle)
	case *pb.Request_QueueRepeat:
		resp = s.handleQueueRepeat(p.QueueRepeat)
	case *pb.Request_QueueMode:
		resp = s.handleQueueMode()
	case *pb.Request_Unsubscribe:
		resp = &pb.Response{Success: true}
	default:
		resp = &pb.Response{Success: false, Error: "unknown request type"}
	}

	resp.RequestId = req.RequestId
	return resp
}

func (s *Server) handlePlay(ctx context.Context, req *pb.PlayRequest) *pb.Response {
	state := s.playback.GetState()
	if state != domain.PlayerStateIdle {
		if err := s.playback.Stop(ctx); err != nil {
			return &pb.Response{Success: false, Error: "failed to stop current track: " + err.Error()}
		}
	}

	switch {
	case req.TrackId != "":
		// Make the played track the current queue item so Next/Prev and the
		// queue panel work during normal library playback. Resolve it first so
		// the queued track carries full metadata.
		if s.queue != nil {
			if track, err := s.libraryRepo.FindByID(ctx, domain.TrackID(req.TrackId)); err == nil {
				s.queue.MoveTo(track)
			}
		}
		if err := s.playback.Play(ctx, domain.TrackID(req.TrackId)); err != nil {
			return &pb.Response{Success: false, Error: err.Error()}
		}
	case req.Query != "":
		if err := s.playback.PlayQuery(ctx, req.Query); err != nil {
			return &pb.Response{Success: false, Error: err.Error()}
		}
	default:
		return &pb.Response{Success: false, Error: "query or track_id required"}
	}

	s.publishPlaybackState(string(s.playback.GetState()))
	s.publishTrackChanged(s.playback.GetCurrentTrack())
	if req.TrackId != "" && s.queue != nil {
		s.publishQueueUpdated()
	}
	return &pb.Response{Success: true}
}

func (s *Server) handlePause(ctx context.Context) *pb.Response {
	if err := s.playback.Pause(ctx); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	s.publishPlaybackState(string(s.playback.GetState()))
	return &pb.Response{Success: true}
}

func (s *Server) handleResume(ctx context.Context) *pb.Response {
	if err := s.playback.Resume(ctx); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	s.publishPlaybackState(string(s.playback.GetState()))
	return &pb.Response{Success: true}
}

func (s *Server) handleStop(ctx context.Context) *pb.Response {
	if err := s.playback.Stop(ctx); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	s.publishPlaybackState(string(s.playback.GetState()))
	s.publishTrackChanged(nil)
	return &pb.Response{Success: true}
}

func (s *Server) handleNext(ctx context.Context) *pb.Response {
	next := s.queue.Next()
	if next == nil {
		return &pb.Response{Success: false, Error: "no next track"}
	}
	if err := s.playback.Play(ctx, next.ID); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	s.publishPlaybackState(string(s.playback.GetState()))
	s.publishTrackChanged(s.playback.GetCurrentTrack())
	return &pb.Response{Success: true}
}

func (s *Server) handlePrev(ctx context.Context) *pb.Response {
	prev := s.queue.Previous()
	if prev == nil {
		return &pb.Response{Success: false, Error: "no previous track"}
	}
	if err := s.playback.Play(ctx, prev.ID); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	s.publishPlaybackState(string(s.playback.GetState()))
	s.publishTrackChanged(s.playback.GetCurrentTrack())
	return &pb.Response{Success: true}
}

func (s *Server) handleSeek(ctx context.Context, req *pb.SeekRequest) *pb.Response {
	if err := s.playback.Seek(ctx, time.Duration(req.OffsetMs)*time.Millisecond); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	return &pb.Response{Success: true}
}

func (s *Server) handleSetVolume(ctx context.Context, req *pb.SetVolumeRequest) *pb.Response {
	if err := s.playback.SetVolume(ctx, int(req.Volume)); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}

	s.publishVolumeChange(req.Volume)
	return &pb.Response{Success: true}
}

func (s *Server) handleQueueAdd(ctx context.Context, req *pb.QueueAddRequest) *pb.Response {
	track, err := s.libraryRepo.FindByID(ctx, domain.TrackID(req.TrackId))
	if err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	if req.Position >= 0 {
		s.queue.Insert(int(req.Position), track)
	} else {
		s.queue.Add(track)
	}
	s.publishQueueUpdated()
	return &pb.Response{Success: true}
}

func (s *Server) handleQueueRemove(req *pb.QueueRemoveRequest) *pb.Response {
	s.queue.Remove(int(req.Position))
	s.publishQueueUpdated()
	return &pb.Response{Success: true}
}

func (s *Server) handleQueueClear() *pb.Response {
	s.queue.Clear()
	s.publishQueueUpdated()
	return &pb.Response{Success: true}
}

func (s *Server) handleSearch(ctx context.Context, req *pb.SearchRequest) *pb.Response {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}

	var tracks []*domain.Track
	var err error
	if req.IncludePeers {
		if rs, ok := s.search.(interface {
			SearchWithRemote(ctx context.Context, query string, limit int) ([]*domain.Track, error)
		}); ok {
			tracks, err = rs.SearchWithRemote(ctx, req.Query, limit)
		} else {
			tracks, err = s.search.Search(ctx, req.Query, limit)
		}
	} else {
		tracks, err = s.search.Search(ctx, req.Query, limit)
	}

	if err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}

	pbTracks := make([]*pb.Track, len(tracks))
	for i, t := range tracks {
		pbTracks[i] = convert.TrackToProto(t)
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_Search{Search: &pb.SearchResponse{Tracks: pbTracks, Total: int32(len(pbTracks))}},
	}
}

func (s *Server) handleLibScanAsync(req *pb.LibScanRequest) *pb.Response {
	jobID := uuid.New().String()
	progressFn := func(p app.ScanProgress) {
		s.publishProgress(jobID, p.Scanned, p.Total, p.CurrentFile, string(p.Phase))
	}
	// Register this scan's own progress handler so concurrent scans don't
	// clobber each other's jobID closure (12.5.5).
	s.scanner.OnProgress(progressFn)

	go func() {
		defer s.scanner.RemoveProgressHandler(progressFn)
		scanCtx, cancel := context.WithCancel(context.Background())
		defer cancel()

		if req.Incremental {
			added, modified, removed, err := s.scanner.ScanIncremental(scanCtx)
			if err != nil {
				slog.Error("background scan failed", "err", err)
				return
			}
			slog.Info("incremental scan complete", "added", added, "modified", modified, "removed", removed)
		} else {
			_, err := s.scanner.Scan(scanCtx)
			if err != nil {
				slog.Error("background scan failed", "err", err)
				return
			}
		}
		s.publishProgress(jobID, 0, 0, "", "complete")
	}()
	return &pb.Response{Success: true, JobId: jobID}
}

func (s *Server) handleListPeers(ctx context.Context) *pb.Response {
	if s.peerRepo == nil {
		return &pb.Response{
			Success: false,
			Error:   errP2PNotEnabled,
		}
	}

	var pbPeers []*pb.Peer
	for p, err := range s.peerRepo.ListAll(ctx) {
		if err != nil {
			continue
		}
		if s.p2pNode != nil {
			if enriched := s.p2pNode.PeerStatus(p); enriched != nil {
				pbPeers = append(pbPeers, enriched)
				continue
			}
		}
		pbPeers = append(pbPeers, convert.PeerInfoToProto(p))
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_ListPeers{ListPeers: &pb.ListPeersResponse{Peers: pbPeers}},
	}
}

func (s *Server) handleListTracks(ctx context.Context, req *pb.ListTracksRequest) *pb.Response {
	if s.libraryRepo == nil {
		return &pb.Response{Success: false, Error: "library is not available"}
	}

	tracks, err := s.libraryRepo.ListAll(ctx)
	if err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}

	offset := int(req.Offset)
	limit := int(req.Limit)
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = len(tracks)
	}

	end := min(offset+limit, len(tracks))
	if offset > len(tracks) {
		offset = len(tracks)
	}
	page := tracks[offset:end]

	pbTracks := make([]*pb.Track, len(page))
	for i, t := range page {
		pbTracks[i] = convert.TrackToProto(t)
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_ListTracks{ListTracks: &pb.ListTracksResponse{
			Tracks: pbTracks,
			Total:  int32(len(tracks)),
		}},
	}
}

func (s *Server) handleQueueList() *pb.Response {
	if s.queue == nil {
		return &pb.Response{Success: false, Error: errQueueUnavailable}
	}

	tracks := s.queue.Tracks()
	pbTracks := make([]*pb.Track, len(tracks))
	for i, t := range tracks {
		pbTracks[i] = convert.TrackToProto(t)
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_QueueList{QueueList: &pb.QueueListResponse{Tracks: pbTracks}},
	}
}

func (s *Server) handleQueueShuffle(req *pb.QueueShuffleRequest) *pb.Response {
	if s.queue == nil {
		return &pb.Response{Success: false, Error: errQueueUnavailable}
	}

	s.queue.SetShuffle(req.Shuffle)
	s.publishQueueUpdated()
	return &pb.Response{Success: true}
}

func (s *Server) handleQueueRepeat(req *pb.QueueRepeatRequest) *pb.Response {
	if s.queue == nil {
		return &pb.Response{Success: false, Error: errQueueUnavailable}
	}

	mode := domain.RepeatMode(req.Mode)
	switch mode {
	case domain.RepeatModeNone, domain.RepeatModeAll, domain.RepeatModeOne:
		s.queue.SetRepeat(mode)
	default:
		return &pb.Response{Success: false, Error: "invalid repeat mode (want \"\", \"all\", or \"one\")"}
	}
	s.publishQueueUpdated()
	return &pb.Response{Success: true}
}

func (s *Server) handleQueueMode() *pb.Response {
	if s.queue == nil {
		return &pb.Response{Success: false, Error: errQueueUnavailable}
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_QueueMode{QueueMode: &pb.QueueModeResponse{
			Shuffle: s.queue.GetShuffle(),
			Repeat:  string(s.queue.GetRepeat()),
		}},
	}
}

func (s *Server) publishQueueUpdated() {
	if s.queue == nil {
		return
	}
	tracks := s.queue.Tracks()
	qt := make([]*pb.Track, len(tracks))
	for i, t := range tracks {
		qt[i] = convert.TrackToProto(t)
	}
	s.PublishEvent(EventQueueUpdated, mustMarshal(&pb.QueueResponse{Tracks: qt}))
}

func (s *Server) handleNetworkStatus() *pb.Response {
	if s.p2pNode == nil {
		return &pb.Response{
			Success: false,
			Error:   errP2PNotEnabled,
		}
	}
	info := s.p2pNode.NetworkInfo()

	var connectedPeers []*pb.ConnectedPeer
	for _, p := range info.ConnectedPeers {
		var addrs []string
		for _, a := range p.Addrs {
			addrs = append(addrs, a.String())
		}
		connectedPeers = append(connectedPeers, &pb.ConnectedPeer{
			PeerId:   p.ID.String(),
			Addrs:    addrs,
			Dialable: true,
		})
	}

	var discoveredPeers []*pb.ConnectedPeer
	for _, p := range info.DiscoveredPeers {
		ps := s.p2pNode.Host().Peerstore().PeerInfo(p.ID)
		dialable := len(ps.Addrs) > 0

		var maddrs []string
		if dialable {
			for _, a := range ps.Addrs {
				maddrs = append(maddrs, a.String())
			}
		} else {
			for _, a := range p.Addrs {
				maddrs = append(maddrs, a.String())
			}
		}

		discoveredPeers = append(discoveredPeers, &pb.ConnectedPeer{
			PeerId:   p.ID.String(),
			Addrs:    maddrs,
			Dialable: dialable,
		})
	}

	return &pb.Response{
		Success: true,
		Payload: &pb.Response_NetworkStatus{NetworkStatus: &pb.NetworkStatusResponse{
			PeerId:          info.PeerID,
			ListenAddrs:     info.ListenAddrs,
			ConnectedPeers:  connectedPeers,
			DiscoveredPeers: discoveredPeers,
		}},
	}
}

func (s *Server) handleBanPeer(req *pb.BanPeerRequest) *pb.Response {
	if s.p2pNode == nil {
		return &pb.Response{
			Success: false,
			Error:   errP2PNotEnabled,
		}
	}
	s.p2pNode.BanPeer(peer.ID(req.PeerId))
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_BanPeer{},
	}
}

func (s *Server) handleUnbanPeer(req *pb.UnbanPeerRequest) *pb.Response {
	if s.p2pNode == nil {
		return &pb.Response{
			Success: false,
			Error:   errP2PNotEnabled,
		}
	}
	s.p2pNode.UnbanPeer(peer.ID(req.PeerId))
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_UnbanPeer{},
	}
}

func (s *Server) handleStatus() *pb.Response {
	state := s.playback.GetState()
	volume := s.playback.GetVolume()
	queueSize := s.queue.Length()
	queuePos := s.queue.Position()
	currentTrack := s.playback.GetCurrentTrack()

	var pbTrack *pb.Track
	if currentTrack != nil {
		pbTrack = convert.TrackToProto(currentTrack)
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_Status{Status: &pb.StatusResponse{
			State:         string(state),
			CurrentTrack:  pbTrack,
			PositionMs:    s.playback.GetPosition().Milliseconds(),
			Volume:        int32(volume),
			QueueLength:   int32(queueSize),
			QueuePosition: int32(queuePos),
		}},
	}
}

func (s *Server) handleHealthCheck() *pb.Response {
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_HealthCheck{HealthCheck: &pb.HealthCheckResponse{Healthy: true}},
	}
}

func (s *Server) handleCreatePlaylist(ctx context.Context, req *pb.CreatePlaylistRequest) *pb.Response {
	if s.playlistRepo == nil {
		return &pb.Response{Success: false, Error: errPlaylistsUnavailable}
	}

	playlist, err := domain.NewPlaylist(req.Name)
	if err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	if err := s.playlistRepo.Save(ctx, playlist); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_CreatePlaylist{
			CreatePlaylist: &pb.CreatePlaylistResponse{
				PlaylistId: string(playlist.ID),
				Name:       playlist.Name,
			},
		},
	}
}

func (s *Server) handleGetPlaylist(ctx context.Context, req *pb.GetPlaylistRequest) *pb.Response {
	if s.playlistRepo == nil {
		return &pb.Response{Success: false, Error: errPlaylistsUnavailable}
	}

	playlist, err := s.playlistRepo.FindByID(ctx, domain.PlaylistID(req.PlaylistId))
	if err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}

	pbPlaylist := convert.PlaylistToProto(playlist)
	resp := &pb.GetPlaylistResponse{Playlist: pbPlaylist}
	if s.libraryRepo != nil {
		for _, trackID := range playlist.TrackIDs {
			track, err := s.libraryRepo.FindByID(ctx, trackID)
			if err != nil {
				continue
			}
			resp.Tracks = append(resp.Tracks, convert.TrackToProto(track))
		}
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_GetPlaylist{GetPlaylist: resp},
	}
}

func (s *Server) handleListPlaylists(ctx context.Context) *pb.Response {
	if s.playlistRepo == nil {
		return &pb.Response{Success: false, Error: errPlaylistsUnavailable}
	}

	var pbPlaylists []*pb.Playlist
	for playlist, err := range s.playlistRepo.ListAll(ctx) {
		if err != nil {
			continue
		}
		pbPlaylists = append(pbPlaylists, convert.PlaylistToProto(playlist))
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_ListPlaylists{ListPlaylists: &pb.ListPlaylistsResponse{Playlists: pbPlaylists}},
	}
}

func (s *Server) handleAddToPlaylist(ctx context.Context, req *pb.AddToPlaylistRequest) *pb.Response {
	if s.playlistRepo == nil {
		return &pb.Response{Success: false, Error: errPlaylistsUnavailable}
	}

	playlist, err := s.playlistRepo.FindByID(ctx, domain.PlaylistID(req.PlaylistId))
	if err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}

	playlist.AddTrack(domain.TrackID(req.TrackId))
	if err := s.playlistRepo.Save(ctx, playlist); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_AddToPlaylist{
			AddToPlaylist: &pb.AddToPlaylistResponse{
				PlaylistId: string(playlist.ID),
				TrackCount: int32(playlist.TrackCount()),
			},
		},
	}
}

func (s *Server) handleGetTrack(ctx context.Context, req *pb.GetTrackRequest) *pb.Response {
	track, err := s.libraryRepo.FindByID(ctx, domain.TrackID(req.TrackId))
	if err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}

	pbTrack := convert.TrackToProto(track)
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_GetTrack{GetTrack: &pb.GetTrackResponse{Track: pbTrack}},
	}
}

func (s *Server) handleGetTrackByPath(ctx context.Context, req *pb.GetTrackByPathRequest) *pb.Response {
	track, err := s.libraryRepo.FindByPath(ctx, req.Path)
	if err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}

	pbTrack := convert.TrackToProto(track)
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_GetTrack{GetTrack: &pb.GetTrackResponse{Track: pbTrack}},
	}
}

func (s *Server) removeConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if i := slices.Index(s.conns, conn); i != -1 {
		s.conns = slices.Delete(s.conns, i, i+1)
	}
	delete(s.connWriteMu, conn)
}

func (s *Server) Stop(ctx context.Context) error {
	s.closeOnce.Do(func() {
		close(s.done)
	})

	if s.listener != nil {
		_ = s.listener.Close()
	}

	s.mu.Lock()
	for _, conn := range s.conns {
		_ = conn.Close()
	}

	s.conns = nil
	s.connWriteMu = nil
	s.mu.Unlock()
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

type SubscriptionManager struct {
	mu   sync.RWMutex
	subs map[net.Conn]*Subscription
}

type Subscription struct {
	conn      net.Conn
	eventMask uint32
	writeMu   sync.Mutex
}

func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		subs: make(map[net.Conn]*Subscription),
	}
}

// Subscribe adds a new subscription
func (sm *SubscriptionManager) Subscribe(conn net.Conn, eventMask uint32) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.subs[conn] = &Subscription{
		conn:      conn,
		eventMask: eventMask,
	}
}

// Unsubscribe removes a subscription
func (sm *SubscriptionManager) Unsubscribe(conn net.Conn) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	delete(sm.subs, conn)
}

// Broadcast sends an event to all subscribers who are interested in this event type
func (sm *SubscriptionManager) Broadcast(eventType uint32, payload []byte) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	event := &pb.Event{
		EventType: pb.EventType(eventType),
		Payload:   payload,
		Timestamp: time.Now().UnixMilli(),
	}

	for _, sub := range sm.subs {
		if sub.eventMask&eventType == 0 {
			continue // Not subscribed to this event type
		}

		sub.writeMu.Lock()
		err := wire.WriteMsg(sub.conn, event)
		sub.writeMu.Unlock()

		if err != nil {
			slog.Debug("event broadcast failed", "error", err)
		}
	}
}

// Count returns the number of active subscriptions
func (sm *SubscriptionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.subs)
}
