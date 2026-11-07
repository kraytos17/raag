package server

import (
	"context"
	"time"

	"github.com/p-society/raag/internal/infra/ipc"

	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

func (s *Server) publishPlaybackState(state string) {
	s.PublishEvent(ipc.EventPlaybackState, []byte(state))
}

// publishTrackChanged broadcasts track change
func (s *Server) publishTrackChanged(track *domain.Track) {
	pbTrack := convert.TrackToProto(track)
	data, _ := proto.Marshal(pbTrack)
	s.PublishEvent(ipc.EventTrackChanged, data)
}

// publishPlaybackProgress broadcasts playback progress (lightweight)
func (s *Server) publishPlaybackProgress(posMs, durMs int64) {
	progress := &pb.ProgressEvent{
		PositionMs: posMs,
		DurationMs: durMs,
	}
	data, _ := proto.Marshal(progress)
	s.PublishEvent(ipc.EventProgress, data)
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
	// The volume event is delivered via the bus (SetVolume publishes
	// ipc.EventVolumeChanged, wired in wireEventBus) — no direct publish here.
	return &pb.Response{Success: true}
}

func (s *Server) handleSetEqualizer(req *pb.EqualizerRequest) *pb.Response {
	if req == nil {
		return &pb.Response{Success: false, Error: "missing equalizer request"}
	}
	if !req.Get {
		s.playback.SetEqualizer(domain.EqualizerSettings{
			Enabled: req.Enabled,
			Bass:    float64(req.BassDb),
			Mid:     float64(req.MidDb),
			Treble:  float64(req.TrebleDb),
		})
	}
	return s.eqResponse()
}

func (s *Server) eqResponse() *pb.Response {
	eq := s.playback.GetEqualizer()
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_Equalizer{Equalizer: &pb.EqualizerResponse{
			Enabled:  eq.Enabled,
			BassDb:   float32(eq.Bass),
			MidDb:    float32(eq.Mid),
			TrebleDb: float32(eq.Treble),
		}},
	}
}

// handleDebugStats aggregates stream-pool, search-index, and DB stats for the
// `raag debug` commands. Unavailable subsystems report zeros (not errors).
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
			BufferFill:    float32(s.playback.GetBufferFillLevel()),
		}},
	}
}

func (s *Server) handleHealthCheck() *pb.Response {
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_HealthCheck{HealthCheck: &pb.HealthCheckResponse{Healthy: true}},
	}
}
