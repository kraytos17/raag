package server

import (
	"context"

	ipcclient "github.com/p-society/raag/internal/infra/ipc/client"

	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	pb "github.com/p-society/raag/proto/gen"
)

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
	s.PublishEvent(ipcclient.EventQueueUpdated, mustMarshal(&pb.QueueResponse{Tracks: qt}))
}
