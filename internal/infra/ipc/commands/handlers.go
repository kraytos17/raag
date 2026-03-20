package commands

import (
	"context"
	"encoding/json"
	"time"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/audio"
)

type handlers struct {
	playback    *app.PlaybackController
	scanner     *app.LibraryScanner
	search      *app.SearchService
	libraryRepo app.LibraryRepository
	peerRepo    app.PeerRepository
	queue       *audio.Queue
}

func NewHandlers(
	playback *app.PlaybackController,
	scanner *app.LibraryScanner,
	search *app.SearchService,
	libraryRepo app.LibraryRepository,
	peerRepo app.PeerRepository,
	queue *audio.Queue,
) *handlers {
	return &handlers{
		playback:    playback,
		scanner:     scanner,
		search:      search,
		libraryRepo: libraryRepo,
		peerRepo:    peerRepo,
		queue:       queue,
	}
}

func (h *handlers) RegisterAll(router *CommandRouter) {
	router.Register(CmdPlay, h.handlePlay)
	router.Register(CmdPause, h.handlePause)
	router.Register(CmdResume, h.handleResume)
	router.Register(CmdStop, h.handleStop)
	router.Register(CmdNext, h.handleNext)
	router.Register(CmdPrev, h.handlePrev)
	router.Register(CmdSeekTo, h.handleSeek)
	router.Register(CmdSetVolume, h.handleSetVolume)
	router.Register(CmdQueueAdd, h.handleQueueAdd)
	router.Register(CmdQueueRemove, h.handleQueueRemove)
	router.Register(CmdQueueClear, h.handleQueueClear)
	router.Register(CmdSearch, h.handleSearch)
	router.Register(CmdLibScan, h.handleLibScan)
	router.Register(CmdListPeers, h.handleListPeers)
	router.Register(CmdStatus, h.handleStatus)
	router.Register(CmdHealthCheck, h.handleHealthCheck)
}

type playRequest struct {
	Query   string `json:"query,omitempty"`
	TrackID string `json:"track_id,omitempty"`
}

func (h *handlers) handlePlay(ctx context.Context, cmd Command) (Response, error) {
	var req playRequest
	if len(cmd.Payload) > 0 {
		if err := json.Unmarshal(cmd.Payload, &req); err != nil {
			return Response{Status: "error", Error: err.Error()}, nil
		}
	}

	switch {
	case req.TrackID != "":
		trackID := domain.TrackID(req.TrackID)
		if err := h.playback.Play(ctx, trackID); err != nil {
			return Response{Status: "error", Error: err.Error()}, err
		}
	case req.Query != "":
		if err := h.playback.PlayQuery(ctx, req.Query); err != nil {
			return Response{Status: "error", Error: err.Error()}, err
		}
	default:
		return Response{Status: "error", Error: "query or track_id required"}, nil
	}
	return Response{Status: "ok"}, nil
}

func (h *handlers) handlePause(ctx context.Context, cmd Command) (Response, error) {
	if err := h.playback.Pause(ctx); err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	return Response{Status: "ok"}, nil
}

func (h *handlers) handleResume(ctx context.Context, cmd Command) (Response, error) {
	if err := h.playback.Resume(ctx); err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	return Response{Status: "ok"}, nil
}

func (h *handlers) handleStop(ctx context.Context, cmd Command) (Response, error) {
	if err := h.playback.Stop(ctx); err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	return Response{Status: "ok"}, nil
}

func (h *handlers) handleNext(ctx context.Context, cmd Command) (Response, error) {
	next := h.queue.Next()
	if next == nil {
		return Response{Status: "error", Error: "no next track"}, nil
	}
	if err := h.playback.Play(ctx, next.ID); err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	return Response{Status: "ok"}, nil
}

func (h *handlers) handlePrev(ctx context.Context, cmd Command) (Response, error) {
	prev := h.queue.Previous()
	if prev == nil {
		return Response{Status: "error", Error: "no previous track"}, nil
	}
	if err := h.playback.Play(ctx, prev.ID); err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	return Response{Status: "ok"}, nil
}

type seekRequest struct {
	OffsetMs int64 `json:"offset_ms"`
}

func (h *handlers) handleSeek(ctx context.Context, cmd Command) (Response, error) {
	var req seekRequest
	if err := json.Unmarshal(cmd.Payload, &req); err != nil {
		return Response{Status: "error", Error: err.Error()}, nil
	}
	if err := h.playback.Seek(ctx, time.Duration(req.OffsetMs)*time.Millisecond); err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	return Response{Status: "ok"}, nil
}

type volumeRequest struct {
	Volume int `json:"volume"`
}

func (h *handlers) handleSetVolume(ctx context.Context, cmd Command) (Response, error) {
	var req volumeRequest
	if err := json.Unmarshal(cmd.Payload, &req); err != nil {
		return Response{Status: "error", Error: err.Error()}, nil
	}
	if err := h.playback.SetVolume(ctx, req.Volume); err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	return Response{Status: "ok"}, nil
}

type queueAddRequest struct {
	TrackID  string `json:"track_id"`
	Position int    `json:"position"`
}

func (h *handlers) handleQueueAdd(ctx context.Context, cmd Command) (Response, error) {
	var req queueAddRequest
	if err := json.Unmarshal(cmd.Payload, &req); err != nil {
		return Response{Status: "error", Error: err.Error()}, nil
	}

	track, err := h.libraryRepo.FindByID(ctx, domain.TrackID(req.TrackID))
	if err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	if req.Position >= 0 {
		h.queue.Insert(req.Position, track)
	} else {
		h.queue.Add(track)
	}
	return Response{Status: "ok"}, nil
}

type queueRemoveRequest struct {
	Position int `json:"position"`
}

func (h *handlers) handleQueueRemove(ctx context.Context, cmd Command) (Response, error) {
	var req queueRemoveRequest
	if err := json.Unmarshal(cmd.Payload, &req); err != nil {
		return Response{Status: "error", Error: err.Error()}, nil
	}
	h.queue.Remove(req.Position)
	return Response{Status: "ok"}, nil
}

func (h *handlers) handleQueueClear(ctx context.Context, cmd Command) (Response, error) {
	h.queue.Clear()
	return Response{Status: "ok"}, nil
}

type searchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (h *handlers) handleSearch(ctx context.Context, cmd Command) (Response, error) {
	var req searchRequest
	if err := json.Unmarshal(cmd.Payload, &req); err != nil {
		return Response{Status: "error", Error: err.Error()}, nil
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}

	tracks, err := h.search.Search(ctx, req.Query, req.Limit)
	if err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}

	data, _ := json.Marshal(tracks)
	return Response{Status: "ok", Data: data}, nil
}

func (h *handlers) handleLibScan(ctx context.Context, cmd Command) (Response, error) {
	_, err := h.scanner.Scan(ctx)
	if err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	return Response{Status: "ok"}, nil
}

func (h *handlers) handleListPeers(ctx context.Context, cmd Command) (Response, error) {
	peers, err := h.peerRepo.ListAllPeers(ctx)
	if err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}

	data, _ := json.Marshal(peers)
	return Response{Status: "ok", Data: data}, nil
}

func (h *handlers) handleStatus(ctx context.Context, cmd Command) (Response, error) {
	status := struct {
		State        app.PlayerState `json:"state"`
		CurrentTrack *domain.Track   `json:"current_track"`
		Position     string          `json:"position"`
		Volume       int             `json:"volume"`
		QueueSize    int             `json:"queue_size"`
		QueuePos     int             `json:"queue_position"`
	}{
		State:     h.playback.GetState(),
		QueueSize: h.queue.Length(),
		QueuePos:  h.queue.Position(),
		Volume:    80,
	}

	if track := h.playback.GetCurrentTrack(); track != nil {
		status.CurrentTrack = track
	}

	data, _ := json.Marshal(status)
	return Response{Status: "ok", Data: data}, nil
}

func (h *handlers) handleHealthCheck(ctx context.Context, cmd Command) (Response, error) {
	health := struct {
		Status string `json:"status"`
	}{Status: "healthy"}

	data, _ := json.Marshal(health)
	return Response{Status: "ok", Data: data}, nil
}
