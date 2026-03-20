package ipc

import (
	"context"
	"errors"
	"iter"
	"log/slog"
	"net"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	pb "github.com/p-society/raag/proto/gen"
)

const (
	ipcReadTimeout  = 30 * time.Second
	ipcWriteTimeout = 5 * time.Second
)

type Server struct {
	socketPath string
	listener   net.Listener

	mu    sync.Mutex
	conns []net.Conn

	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	playback    PlaybackHandler
	scanner     ScannerHandler
	search      SearchHandler
	libraryRepo LibraryRepoHandler
	peerRepo    PeerRepoHandler
	queue       QueueHandler
}

type PlaybackHandler interface {
	Play(ctx context.Context, trackID domain.TrackID) error
	PlayQuery(ctx context.Context, query string) error
	Pause(ctx context.Context) error
	Resume(ctx context.Context) error
	Stop(ctx context.Context) error
	Seek(ctx context.Context, dur time.Duration) error
	SetVolume(ctx context.Context, vol int) error
	GetState() app.PlayerState
	GetVolume() int
	GetCurrentTrack() *domain.Track
}

type ScannerHandler interface {
	Scan(ctx context.Context) (int, error)
	ScanIncremental(ctx context.Context) (added, modified, removed int, err error)
}

type SearchHandler interface {
	Search(ctx context.Context, query string, limit int) ([]*domain.Track, error)
}

type LibraryRepoHandler interface {
	FindByID(ctx context.Context, trackID domain.TrackID) (*domain.Track, error)
	FindByPath(ctx context.Context, path string) (*domain.Track, error)
}

type PeerRepoHandler interface {
	ListAll(ctx context.Context) iter.Seq[*domain.PeerInfo]
}

type QueueHandler interface {
	Add(track *domain.Track)
	Insert(pos int, track *domain.Track)
	Remove(pos int)
	Clear()
	Length() int
	Position() int
	Next() *domain.Track
	Previous() *domain.Track
}

func NewServer(socketPath string) *Server {
	return &Server{
		socketPath: socketPath,
		done:       make(chan struct{}),
	}
}

func (s *Server) SetPlaybackHandler(h PlaybackHandler) {
	s.playback = h
}

func (s *Server) SetScannerHandler(h ScannerHandler) {
	s.scanner = h
}

func (s *Server) SetSearchHandler(h SearchHandler) {
	s.search = h
}

func (s *Server) SetLibraryRepoHandler(h LibraryRepoHandler) {
	s.libraryRepo = h
}

func (s *Server) SetPeerRepoHandler(h PeerRepoHandler) {
	s.peerRepo = h
}

func (s *Server) SetQueueHandler(h QueueHandler) {
	s.queue = h
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

		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()

		s.wg.Add(1)
		go s.handleConn(ctx, conn)
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		_ = conn.Close()
		s.removeConn(conn)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		if err := conn.SetReadDeadline(time.Now().Add(ipcReadTimeout)); err != nil {
			return
		}

		var req pb.Request
		if err := ReadMsg(conn, &req); err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return
		}

		resp := s.dispatch(ctx, &req)
		if err := conn.SetWriteDeadline(time.Now().Add(ipcWriteTimeout)); err != nil {
			return
		}
		if err := WriteMsg(conn, resp); err != nil {
			slog.Warn("write response failed", "error", err)
			return
		}
	}
}

func (s *Server) dispatch(ctx context.Context, req *pb.Request) *pb.Response {
	switch p := req.Payload.(type) {
	case *pb.Request_Empty:
		return s.handleEmpty()
	case *pb.Request_Play:
		return s.handlePlay(ctx, p.Play)
	case *pb.Request_Pause:
		return s.handlePause(ctx)
	case *pb.Request_Resume:
		return s.handleResume(ctx)
	case *pb.Request_Stop:
		return s.handleStop(ctx)
	case *pb.Request_Next:
		return s.handleNext(ctx)
	case *pb.Request_Prev:
		return s.handlePrev(ctx)
	case *pb.Request_Seek:
		return s.handleSeek(ctx, p.Seek)
	case *pb.Request_SetVolume:
		return s.handleSetVolume(ctx, p.SetVolume)
	case *pb.Request_QueueAdd:
		return s.handleQueueAdd(ctx, p.QueueAdd)
	case *pb.Request_QueueRemove:
		return s.handleQueueRemove(p.QueueRemove)
	case *pb.Request_QueueClear:
		return s.handleQueueClear()
	case *pb.Request_Search:
		return s.handleSearch(ctx, p.Search)
	case *pb.Request_LibScan:
		return s.handleLibScan(ctx, p.LibScan)
	case *pb.Request_ListPeers:
		return s.handleListPeers(ctx)
	case *pb.Request_Status:
		return s.handleStatus()
	case *pb.Request_HealthCheck:
		return s.handleHealthCheck()
	case *pb.Request_CreatePlaylist:
		return s.handleCreatePlaylist(p.CreatePlaylist)
	case *pb.Request_GetPlaylist:
		return s.handleGetPlaylist(p.GetPlaylist)
	case *pb.Request_ListPlaylists:
		return s.handleListPlaylists()
	case *pb.Request_AddToPlaylist:
		return s.handleAddToPlaylist(p.AddToPlaylist)
	case *pb.Request_GetTrack:
		return s.handleGetTrack(ctx, p.GetTrack)
	case *pb.Request_GetTrackByPath:
		return s.handleGetTrackByPath(ctx, p.GetTrackByPath)
	default:
		return &pb.Response{Success: false, Error: "unknown request type"}
	}
}

func (s *Server) handleEmpty() *pb.Response {
	return &pb.Response{Success: true}
}

func (s *Server) handlePlay(ctx context.Context, req *pb.PlayRequest) *pb.Response {
	switch {
	case req.TrackId != "":
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
	return &pb.Response{Success: true}
}

func (s *Server) handlePause(ctx context.Context) *pb.Response {
	if err := s.playback.Pause(ctx); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	return &pb.Response{Success: true}
}

func (s *Server) handleResume(ctx context.Context) *pb.Response {
	if err := s.playback.Resume(ctx); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
	return &pb.Response{Success: true}
}

func (s *Server) handleStop(ctx context.Context) *pb.Response {
	if err := s.playback.Stop(ctx); err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}
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
	return &pb.Response{Success: true}
}

func (s *Server) handleQueueRemove(req *pb.QueueRemoveRequest) *pb.Response {
	s.queue.Remove(int(req.Position))
	return &pb.Response{Success: true}
}

func (s *Server) handleQueueClear() *pb.Response {
	s.queue.Clear()
	return &pb.Response{Success: true}
}

func (s *Server) handleSearch(ctx context.Context, req *pb.SearchRequest) *pb.Response {
	limit := int(req.Limit)
	if limit <= 0 {
		limit = 20
	}

	tracks, err := s.search.Search(ctx, req.Query, limit)
	if err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}

	pbTracks := make([]*pb.Track, len(tracks))
	for i, t := range tracks {
		pbTracks[i] = &pb.Track{
			Id:         string(t.ID),
			Path:       t.Path,
			Title:      t.Title,
			Artist:     t.Artist,
			Album:      t.Album,
			DurationMs: t.DurationMs,
			AddedAt:    t.AddedAt,
			ModifiedAt: t.ModifiedAt,
		}
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_Search{Search: &pb.SearchResponse{Tracks: pbTracks, Total: int32(len(pbTracks))}},
	}
}

func (s *Server) handleLibScan(ctx context.Context, req *pb.LibScanRequest) *pb.Response {
	if req.Incremental {
		added, modified, removed, err := s.scanner.ScanIncremental(ctx)
		if err != nil {
			return &pb.Response{Success: false, Error: err.Error()}
		}
		slog.Info("incremental scan complete", "added", added, "modified", modified, "removed", removed)
	} else {
		_, err := s.scanner.Scan(ctx)
		if err != nil {
			return &pb.Response{Success: false, Error: err.Error()}
		}
	}
	return &pb.Response{Success: true}
}

func (s *Server) handleListPeers(ctx context.Context) *pb.Response {
	var pbPeers []*pb.Peer
	for p := range s.peerRepo.ListAll(ctx) {
		pbPeers = append(pbPeers, &pb.Peer{Id: string(p.ID), Addrs: p.Addrs})
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_ListPeers{ListPeers: &pb.ListPeersResponse{Peers: pbPeers}},
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
		pbTrack = &pb.Track{
			Id:     string(currentTrack.ID),
			Title:  currentTrack.Title,
			Artist: currentTrack.Artist,
			Album:  currentTrack.Album,
		}
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_Status{Status: &pb.StatusResponse{
			State:         string(state),
			CurrentTrack:  pbTrack,
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

func (s *Server) handleCreatePlaylist(req *pb.CreatePlaylistRequest) *pb.Response {
	return &pb.Response{Success: false, Error: "not implemented"}
}

func (s *Server) handleGetPlaylist(req *pb.GetPlaylistRequest) *pb.Response {
	return &pb.Response{Success: false, Error: "not implemented"}
}

func (s *Server) handleListPlaylists() *pb.Response {
	return &pb.Response{Success: false, Error: "not implemented"}
}

func (s *Server) handleAddToPlaylist(req *pb.AddToPlaylistRequest) *pb.Response {
	return &pb.Response{Success: false, Error: "not implemented"}
}

func (s *Server) handleGetTrack(ctx context.Context, req *pb.GetTrackRequest) *pb.Response {
	track, err := s.libraryRepo.FindByID(ctx, domain.TrackID(req.TrackId))
	if err != nil {
		return &pb.Response{Success: false, Error: err.Error()}
	}

	pbTrack := &pb.Track{
		Id:         string(track.ID),
		Path:       track.Path,
		Title:      track.Title,
		Artist:     track.Artist,
		Album:      track.Album,
		DurationMs: track.DurationMs,
		AddedAt:    track.AddedAt,
		ModifiedAt: track.ModifiedAt,
	}
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

	pbTrack := &pb.Track{
		Id:         string(track.ID),
		Path:       track.Path,
		Title:      track.Title,
		Artist:     track.Artist,
		Album:      track.Album,
		DurationMs: track.DurationMs,
		AddedAt:    track.AddedAt,
		ModifiedAt: track.ModifiedAt,
	}
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
	s.mu.Unlock()
	s.wg.Wait()
	return os.Remove(s.socketPath)
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
