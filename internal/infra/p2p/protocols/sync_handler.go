package protocols

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

const (
	SyncProtocol   = "/raag/sync/1.0.0"
	StreamProtocol = "/raag/stream/1.0.0"
)

type CapabilitiesProvider interface {
	GetLocalCapabilities() *pb.PeerCapabilities
}

type SyncHandler struct {
	mu              sync.RWMutex
	library         app.LibraryRepository
	search          app.SearchHandler
	localPeerID     peer.ID
	capsProvider    CapabilitiesProvider
	announceLibrary bool
	admission       *AdmissionRegistry
}

func NewSyncHandler(library app.LibraryRepository, search app.SearchHandler, localPeerID peer.ID) *SyncHandler {
	return &SyncHandler{
		library:     library,
		search:      search,
		localPeerID: localPeerID,
	}
}

func (h *SyncHandler) SetCapabilitiesProvider(provider CapabilitiesProvider) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.capsProvider = provider
}

func (h *SyncHandler) SetAdmissionRegistry(admission *AdmissionRegistry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.admission = admission
}

func (h *SyncHandler) SetAnnounceLibrary(announce bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.announceLibrary = announce
}

func (h *SyncHandler) Handle(stream network.Stream) {
	defer func() {
		if err := stream.Close(); err != nil {
			slog.Debug("stream close error", "err", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := stream.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		slog.Debug("failed to set read deadline", "err", err)
	}

	var req pb.SyncRequest
	if err := wire.ReadMsg(stream, &req); err != nil {
		// Clean peer close (EOF) or a stream/connection reset (client
		// abort, or the host closing its connections during daemon shutdown)
		// are benign end-of-stream conditions, not serving failures.
		if errors.Is(err, io.EOF) || errors.Is(err, network.ErrReset) {
			return
		}

		slog.Error("failed to read sync request", "err", err)
		if err := stream.Reset(); err != nil {
			slog.Debug("stream reset error", "err", err)
		}
		return
	}
	if err := stream.SetReadDeadline(time.Time{}); err != nil {
		slog.Debug("failed to clear read deadline", "err", err)
	}

	peerID := stream.Conn().RemotePeer()
	switch payload := req.Payload.(type) {
	case *pb.SyncRequest_ManifestRequest:
		if h.admission != nil {
			h.admission.Admit(peerID)
		}
		h.handleManifestRequest(ctx, stream, peerID)
	case *pb.SyncRequest_TrackDetailRequest:
		if h.admission == nil || !h.admission.IsAdmitted(peerID) {
			h.sendError(stream, "manifest exchange required", pb.ErrorCode_ERROR_CODE_UNAUTHORIZED)
			return
		}
		h.handleTrackDetailRequest(ctx, stream, payload.TrackDetailRequest.TrackId)
	case *pb.SyncRequest_RemoteSearchRequest:
		if h.admission == nil || !h.admission.IsAdmitted(peerID) {
			h.sendError(stream, "manifest exchange required", pb.ErrorCode_ERROR_CODE_UNAUTHORIZED)
			return
		}
		h.handleRemoteSearchRequest(ctx, stream, payload.RemoteSearchRequest.Query, int(payload.RemoteSearchRequest.Limit))
	case *pb.SyncRequest_CapabilitiesRequest:
		h.handleCapabilitiesRequest(ctx, stream)
	default:
		h.sendError(stream, "unknown request type", pb.ErrorCode_ERROR_CODE_UNSPECIFIED)
	}
}

func (h *SyncHandler) handleManifestRequest(ctx context.Context, stream network.Stream, _ peer.ID) {
	h.mu.RLock()
	announce := h.announceLibrary
	h.mu.RUnlock()

	if !announce {
		// Return a real error so the requesting peer's FetchPeerData can
		// distinguish "sharing disabled" from a legitimately empty library
		// (which returns a manifest with no track IDs). This makes the client's
		// "library sharing disabled" branch reachable
		h.sendError(stream, "library sharing disabled", pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED)
		slog.Debug("manifest request refused (sharing disabled)")
		return
	}

	tracks, err := h.library.ListAll(ctx)
	if err != nil {
		h.sendError(stream, "failed to get track list", pb.ErrorCode_ERROR_CODE_UNSPECIFIED)
		return
	}

	trackIDs := make([]string, len(tracks))
	for i, t := range tracks {
		trackIDs[i] = string(t.ID)
	}

	resp := &pb.SyncResponse{
		Payload: &pb.SyncResponse_Manifest{
			Manifest: &pb.LibraryManifest{
				PeerId:    h.localPeerID.String(),
				TrackIds:  trackIDs,
				Timestamp: time.Now().Unix(),
			},
		},
	}
	if err := wire.WriteMsg(stream, resp); err != nil {
		slog.Error("failed to send manifest", "err", err)
	}
	slog.Debug("manifest sent", "tracks", len(trackIDs))
}

func (h *SyncHandler) handleTrackDetailRequest(ctx context.Context, stream network.Stream, trackID string) {
	track, err := h.library.FindByID(ctx, domain.TrackID(trackID))
	if err != nil {
		h.sendError(stream, "track not found", pb.ErrorCode_ERROR_CODE_TRACK_NOT_FOUND)
		return
	}

	resp := &pb.SyncResponse{
		Payload: &pb.SyncResponse_Track{
			Track: convert.TrackToProto(track),
		},
	}
	if err := wire.WriteMsg(stream, resp); err != nil {
		slog.Error("failed to send track", "err", err)
	}
}

func (h *SyncHandler) handleRemoteSearchRequest(ctx context.Context, stream network.Stream, query string, limit int) {
	if h.search == nil {
		h.sendError(stream, "search unavailable", pb.ErrorCode_ERROR_CODE_UNSPECIFIED)
		return
	}
	if limit <= 0 {
		limit = 20
	}

	tracks, err := h.search.Search(ctx, query, limit)
	if err != nil {
		h.sendError(stream, "search failed: "+err.Error(), pb.ErrorCode_ERROR_CODE_UNSPECIFIED)
		return
	}

	pbTracks := make([]*pb.Track, len(tracks))
	for i, t := range tracks {
		pbTracks[i] = convert.TrackToProto(t)
	}

	resp := &pb.SyncResponse{
		Payload: &pb.SyncResponse_Search{
			Search: &pb.SearchResult{
				Tracks: pbTracks,
			},
		},
	}
	if err := wire.WriteMsg(stream, resp); err != nil {
		slog.Error("failed to send search results", "err", err)
	}
}

func (h *SyncHandler) handleCapabilitiesRequest(_ context.Context, stream network.Stream) {
	h.mu.RLock()
	provider := h.capsProvider
	h.mu.RUnlock()

	var caps *pb.PeerCapabilities
	if provider != nil {
		caps = provider.GetLocalCapabilities()
	}
	if caps == nil {
		caps = &pb.PeerCapabilities{
			SupportedCodecs:   domain.PlayableCodecs,
			SupportedBitrates: domain.SupportedBitrates,
			ProtocolVersion:   "1.0.0",
		}
	}

	resp := &pb.SyncResponse{
		Payload: &pb.SyncResponse_Capabilities{
			Capabilities: caps,
		},
	}
	if err := wire.WriteMsg(stream, resp); err != nil {
		slog.Error("failed to send capabilities", "err", err)
	}
}

func (h *SyncHandler) sendError(stream network.Stream, msg string, code pb.ErrorCode) {
	resp := &pb.SyncResponse{
		Payload: &pb.SyncResponse_Error{
			Error: &pb.ErrorResponse{
				Message: msg,
				Code:    code,
			},
		},
	}

	_ = wire.WriteMsg(stream, resp)
	if err := stream.Reset(); err != nil {
		slog.Debug("stream reset error in sendError", "err", err)
	}
}
