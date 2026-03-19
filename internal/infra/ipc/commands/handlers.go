package commands

import (
	"context"
	"encoding/json"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
)

type handlers struct {
	playback    *app.PlaybackController
	scanner     *app.LibraryScanner
	search      *app.SearchService
	libraryRepo app.LibraryRepository
	peerRepo    app.PeerRepository
	queue       *queue
}

func newHandlers(
	playback *app.PlaybackController,
	scanner *app.LibraryScanner,
	search *app.SearchService,
	libraryRepo app.LibraryRepository,
	peerRepo app.PeerRepository,
) *handlers {
	return &handlers{
		playback:    playback,
		scanner:     scanner,
		search:      search,
		libraryRepo: libraryRepo,
		peerRepo:    peerRepo,
		queue:       newQueue(),
	}
}

type playRequest struct {
	Query   string `json:"query,omitempty"`
	TrackID string `json:"track_id,omitempty"`
}

type playHandler struct {
	*handlers
}

func (h *playHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
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

type pauseHandler struct{}

func (h *pauseHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	return Response{Status: "ok"}, nil
}

type resumeHandler struct{}

func (h *resumeHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	return Response{Status: "ok"}, nil
}

type stopHandler struct{}

func (h *stopHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	return Response{Status: "ok"}, nil
}

type searchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

type searchHandler struct {
	*handlers
}

func (h *searchHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
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

type libScanHandler struct {
	*handlers
}

func (h *libScanHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	_, err := h.scanner.Scan(ctx)
	if err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}
	return Response{Status: "ok"}, nil
}

type listPeersHandler struct {
	*handlers
}

func (h *listPeersHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	peers, err := h.peerRepo.ListAllPeers(ctx)
	if err != nil {
		return Response{Status: "error", Error: err.Error()}, err
	}

	data, _ := json.Marshal(peers)
	return Response{Status: "ok", Data: data}, nil
}

type statusHandler struct {
	*handlers
}

func (h *statusHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
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

type healthCheckHandler struct{}

func (h *healthCheckHandler) Handle(ctx context.Context, cmd Command) (Response, error) {
	health := struct {
		Status string `json:"status"`
	}{"healthy"}

	data, _ := json.Marshal(health)
	return Response{Status: "ok", Data: data}, nil
}

type queue struct {
	tracks []domain.TrackID
	pos    int
}

func newQueue() *queue {
	return &queue{
		tracks: make([]domain.TrackID, 0),
		pos:    -1,
	}
}

func (q *queue) Add(trackID domain.TrackID) {
	q.tracks = append(q.tracks, trackID)
}

func (q *queue) Next() *domain.TrackID {
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

func (q *queue) Prev() *domain.TrackID {
	if len(q.tracks) == 0 || q.pos <= 0 {
		return nil
	}

	q.pos--
	return &q.tracks[q.pos]
}

func (q *queue) Len() int {
	return len(q.tracks)
}

func (q *queue) Clear() {
	q.tracks = make([]domain.TrackID, 0)
	q.pos = -1
}

func registerAll(router *CommandRouter, h *handlers) {
	router.Register(CmdPlay, (&playHandler{handlers: h}).Handle)
	router.Register(CmdPause, (&pauseHandler{}).Handle)
	router.Register(CmdResume, (&resumeHandler{}).Handle)
	router.Register(CmdStop, (&stopHandler{}).Handle)
	router.Register(CmdSearch, (&searchHandler{handlers: h}).Handle)
	router.Register(CmdLibScan, (&libScanHandler{handlers: h}).Handle)
	router.Register(CmdListPeers, (&listPeersHandler{handlers: h}).Handle)
	router.Register(CmdStatus, (&statusHandler{handlers: h}).Handle)
	router.Register(CmdHealthCheck, (&healthCheckHandler{}).Handle)
}
