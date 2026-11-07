package server

import (
	"context"

	"github.com/p-society/raag/internal/app"
	pb "github.com/p-society/raag/proto/gen"
)

func (s *Server) handleDebugStats() *pb.Response {
	var streams *pb.DebugStreams
	if s.p2pNode != nil {
		st := s.p2pNode.StreamPool().Stats()
		streams = &pb.DebugStreams{
			TotalStreams: int32(st.TotalStreams),
			PeerCount:    int32(st.PeerCount),
		}
	}

	var index *pb.DebugIndex
	if si, ok := s.search.(interface {
		Stats(ctx context.Context) (app.IndexStats, error)
	}); ok {
		if st, err := si.Stats(context.Background()); err == nil {
			index = &pb.DebugIndex{
				TotalTracks:   int32(st.TotalTracks),
				TotalTerms:    int32(st.TotalTerms),
				TotalTrigrams: int32(st.TotalTrigrams),
				LastUpdated:   st.LastUpdated,
			}
		}
	}

	var dbInfo *pb.DebugDB
	if s.dbStats != nil {
		lsm, vlog := s.dbStats.Size()
		dbInfo = &pb.DebugDB{LsmBytes: lsm, VlogBytes: vlog}
	}

	return &pb.Response{
		Success: true,
		Payload: &pb.Response_DebugStats{DebugStats: &pb.DebugStatsResponse{
			Streams: streams,
			Index:   index,
			Db:      dbInfo,
		}},
	}
}
