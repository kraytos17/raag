package commands

import (
	"context"
	"encoding/json"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

type Handlers struct {
	playback    *app.PlaybackController
	scanner     *app.LibraryScanner
	search      *app.SearchService
	libraryRepo app.LibraryRepository
	peerRepo    app.PeerRepository
	queue       *Queue
}

func NewHandlers(
	playback *app.PlaybackController,
	scanner *app.LibraryScanner,
	search *app.SearchService,
	libraryRepo app.LibraryRepository,
	peerRepo app.PeerRepository,
) *Handlers {
	return &Handlers{
		playback:    playback,
		scanner:     scanner,
		search:      search,
		libraryRepo: libraryRepo,
		peerRepo:    peerRepo,
		queue:       NewQueue(),
	}
}

type PlayRequest struct {
	Query   string `json:"query,omitempty"`
	TrackID string `json:"track_id,omitempty"`
}

type PlayHandler struct {
	*Handlers
}

func (h *PlayHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	var req PlayRequest
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

type PauseHandler struct{}

func (h *PauseHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	return Response{Status: "ok"}, nil
}

type ResumeHandler struct{}

func (h *ResumeHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	return Response{Status: "ok"}, nil
}

type StopHandler struct{}

func (h *StopHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	return Response{Status: "ok"}, nil
}

type SearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type SearchHandler struct {
	*Handlers
}

func (h *SearchHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	var req SearchRequest
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

type LibScanHandler struct {
	*Handlers
}

func (h *LibScanHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	_, err := h.scanner.Scan(ctx)
	if err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	return Response{Status: "ok"}, nil
}

type ListPeersHandler struct {
	*Handlers
}

func (h *ListPeersHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	peers, err := h.peerRepo.ListAllPeers(ctx)
	if err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}

	data, _ := json.Marshal(peers)
	return Response{Status: "ok", Data: data}, nil
}

type StatusHandler struct {
	*Handlers
}

func (h *StatusHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	status := struct {
		State     app.PlayerState `json:"state"`
		Volume    int             `json:"volume"`
		QueueSize int             `json:"queue_size"`
	}{
		State:     h.playback.GetState(),
		Volume:    80,
		QueueSize: h.queue.Len(),
	}

	data, _ := json.Marshal(status)
	return Response{Status: "ok", Data: data}, nil
}

type HealthCheckHandler struct{}

func (h *HealthCheckHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	health := struct {
		Status string `json:"status"`
	}{"healthy"}

	data, _ := json.Marshal(health)
	return Response{Status: "ok", Data: data}, nil
}

type Queue struct {
	tracks []domain.TrackID
	pos    int
}

func NewQueue() *Queue {
	return &Queue{
		tracks: make([]domain.TrackID, 0),
		pos:    -1,
	}
}

func (q *Queue) Add(trackID domain.TrackID) {
	q.tracks = append(q.tracks, trackID)
}

func (q *Queue) Next() *domain.TrackID {
	if len(q.tracks) == 0 {
		return nil
	}

	q.pos++
	if q.pos >= len(q.tracks) {
		q.pos = len(q.tracks) - 1
		return nil
	}
	return &q.tracks[q.pos]
}

func (q *Queue) Prev() *domain.TrackID {
	if len(q.tracks) == 0 || q.pos <= 0 {
		return nil
	}

	q.pos--
	return &q.tracks[q.pos]
}

func (q *Queue) Len() int {
	return len(q.tracks)
}

func (q *Queue) Clear() {
	q.tracks = make([]domain.TrackID, 0)
	q.pos = -1
}

func RegisterAll(router *CommandRouter, handlers *Handlers) {
	router.Register(CmdPlay, (&PlayHandler{Handlers: handlers}).Handle)
	router.Register(CmdPause, (&PauseHandler{}).Handle)
	router.Register(CmdResume, (&ResumeHandler{}).Handle)
	router.Register(CmdStop, (&StopHandler{}).Handle)
	router.Register(CmdSearch, (&SearchHandler{Handlers: handlers}).Handle)
	router.Register(CmdLibScan, (&LibScanHandler{Handlers: handlers}).Handle)
	router.Register(CmdListPeers, (&ListPeersHandler{Handlers: handlers}).Handle)
	router.Register(CmdStatus, (&StatusHandler{Handlers: handlers}).Handle)
	router.Register(CmdHealthCheck, (&HealthCheckHandler{}).Handle)
}
