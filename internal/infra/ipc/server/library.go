package server

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	pb "github.com/p-society/raag/proto/gen"
)

func (s *Server) SetProgressCallback(fn func(jobID string, scanned int, total int, currentFile string, phase string)) {
	s.progressCbMu.Lock()
	defer s.progressCbMu.Unlock()
	s.progressCb = fn
}

// PublishEvent broadcasts an event to all subscribed clients
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
	// clobber each other's jobID closure
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
