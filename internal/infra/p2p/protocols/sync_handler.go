package protocols

import (
	"context"
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
	allowed         map[peer.ID]bool
	localPeerID     peer.ID
	capsProvider    CapabilitiesProvider
	announceLibrary bool
}

func NewSyncHandler(library app.LibraryRepository, localPeerID peer.ID) *SyncHandler {
	return &SyncHandler{
		library:     library,
		allowed:     make(map[peer.ID]bool),
		localPeerID: localPeerID,
	}
}

func (h *SyncHandler) SetCapabilitiesProvider(provider CapabilitiesProvider) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.capsProvider = provider
}

func (h *SyncHandler) SetAnnounceLibrary(announce bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.announceLibrary = announce
}

func (h *SyncHandler) Allow(peerID peer.ID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.allowed[peerID] = true
}

func (h *SyncHandler) Deny(peerID peer.ID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.allowed, peerID)
}

func (h *SyncHandler) Handle(stream network.Stream) {
	defer stream.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream.SetReadDeadline(time.Now().Add(30 * time.Second))
	var req pb.SyncRequest
	if err := wire.ReadMsg(stream, &req); err != nil {
		slog.Error("failed to read sync request", "err", err)
		stream.Reset()
		return
	}

	stream.SetReadDeadline(time.Time{})
	peerID := stream.Conn().RemotePeer()
	if !h.isAllowed(peerID) {
		h.sendError(stream, "permission denied")
		return
	}

	switch payload := req.Payload.(type) {
	case *pb.SyncRequest_ManifestRequest:
		h.handleManifestRequest(ctx, stream, peerID)
	case *pb.SyncRequest_TrackDetailRequest:
		h.handleTrackDetailRequest(ctx, stream, payload.TrackDetailRequest.TrackId)
	case *pb.SyncRequest_CapabilitiesRequest:
		h.handleCapabilitiesRequest(ctx, stream)
	default:
		h.sendError(stream, "unknown request type")
	}
}

func (h *SyncHandler) isAllowed(peerID peer.ID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.allowed[peerID]
}

func (h *SyncHandler) handleManifestRequest(ctx context.Context, stream network.Stream, _ peer.ID) {
	h.mu.RLock()
	announce := h.announceLibrary
	h.mu.RUnlock()

	if !announce {
		slog.Debug("manifest request denied: library sharing disabled")
		h.sendError(stream, "library sharing disabled")
		return
	}

	tracks, err := h.library.ListAll(ctx)
	if err != nil {
		h.sendError(stream, "failed to get track list")
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
		h.sendError(stream, "track not found")
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
			SupportedCodecs:   domain.AudioExtensions,
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

func (h *SyncHandler) sendError(stream network.Stream, msg string) {
	resp := &pb.SyncResponse{
		Payload: &pb.SyncResponse_Error{
			Error: &pb.ErrorResponse{
				Message: msg,
				Code:    pb.ErrorCode_ERROR_CODE_UNSPECIFIED,
			},
		},
	}

	_ = wire.WriteMsg(stream, resp)
	stream.Reset()
}
