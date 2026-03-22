package protocols

import (
	"context"
	"io"
	"log/slog"
	"os"
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
	MaxChunkSize   = 256 * 1024
)

const MaxMessageSize = wire.MaxMessageSize

type StreamHandler struct {
	mu      sync.RWMutex
	library app.LibraryRepository
	allowed map[peer.ID]bool
}

func NewStreamHandler(library app.LibraryRepository) *StreamHandler {
	return &StreamHandler{
		library: library,
		allowed: make(map[peer.ID]bool),
	}
}

func (h *StreamHandler) Allow(peerID peer.ID) {
	h.mu.Lock()
	h.allowed[peerID] = true
	h.mu.Unlock()
}

func (h *StreamHandler) Deny(peerID peer.ID) {
	h.mu.Lock()
	delete(h.allowed, peerID)
	h.mu.Unlock()
}

func (h *StreamHandler) Handle(stream network.Stream) {
	defer stream.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var req pb.ChunkRequest
	if err := wire.ReadMsg(stream, &req); err != nil {
		slog.Error("failed to read chunk request", "err", err)
		stream.Reset()
		return
	}

	peerID := stream.Conn().RemotePeer()
	if !h.isAllowed(peerID) {
		slog.Warn("unauthorized stream request", "peer", peerID)
		h.sendError(stream, req.TrackId, "permission denied", pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED)
		return
	}
	h.serveChunk(ctx, stream, &req)
}

func (h *StreamHandler) isAllowed(peerID peer.ID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.allowed[peerID]
}

func (h *StreamHandler) serveChunk(ctx context.Context, stream network.Stream, req *pb.ChunkRequest) {
	track, err := h.library.FindByID(ctx, domain.TrackID(req.TrackId))
	if err != nil {
		slog.Error("track not found", "track_id", req.TrackId)
		h.sendError(stream, req.TrackId, "track not found", pb.ErrorCode_ERROR_CODE_TRACK_NOT_FOUND)
		return
	}

	f, err := os.Open(track.Path)
	if err != nil {
		slog.Error("failed to open track", "path", track.Path)
		h.sendError(stream, req.TrackId, "file error", pb.ErrorCode_ERROR_CODE_UNSPECIFIED)
		return
	}
	defer f.Close()

	if req.Offset > 0 {
		if _, err := f.Seek(req.Offset, io.SeekStart); err != nil {
			slog.Error("seek failed", "offset", req.Offset)
			h.sendError(stream, req.TrackId, "seek error", pb.ErrorCode_ERROR_CODE_UNSPECIFIED)
			return
		}
	}

	length := min(int(req.Length), MaxChunkSize)
	data := make([]byte, length)
	n, err := io.ReadFull(f, data)
	if err != nil && err != io.EOF {
		slog.Error("read failed", "err", err)
		h.sendError(stream, req.TrackId, "read error", pb.ErrorCode_ERROR_CODE_UNSPECIFIED)
		return
	}

	lastChunk := n < int(req.Length) || err == io.EOF
	resp := &pb.ChunkResponse{
		TrackId:   req.TrackId,
		Offset:    req.Offset,
		Data:      data[:n],
		LastChunk: lastChunk,
		TotalSize: int64(track.SizeBytes),
		MimeType:  track.MimeType,
	}
	if err := wire.WriteMsg(stream, resp); err != nil {
		slog.Error("write response failed", "err", err)
		return
	}
}

func (h *StreamHandler) sendError(stream network.Stream, trackID string, msg string, code pb.ErrorCode) {
	slog.Debug("stream error", "track_id", trackID, "error", msg, "code", code)
	resp := &pb.SyncResponse{
		Payload: &pb.SyncResponse_Error{
			Error: &pb.ErrorResponse{
				Message: msg,
				Code:    code,
			},
		},
	}

	_ = wire.WriteMsg(stream, resp)
	stream.Reset()
}

type SyncHandler struct {
	mu          sync.RWMutex
	library     app.LibraryRepository
	allowed     map[peer.ID]bool
	localPeerID peer.ID
}

func NewSyncHandler(library app.LibraryRepository, localPeerID peer.ID) *SyncHandler {
	return &SyncHandler{
		library:     library,
		allowed:     make(map[peer.ID]bool),
		localPeerID: localPeerID,
	}
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

	var req pb.SyncRequest
	if err := wire.ReadMsg(stream, &req); err != nil {
		slog.Error("failed to read sync request", "err", err)
		stream.Reset()
		return
	}

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
	}
}

func (h *SyncHandler) isAllowed(peerID peer.ID) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.allowed[peerID]
}

func (h *SyncHandler) handleManifestRequest(ctx context.Context, stream network.Stream, _ peer.ID) {
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
				PeerId:   h.localPeerID.String(),
				TrackIds: trackIDs,
			},
		},
	}
	if err := wire.WriteMsg(stream, resp); err != nil {
		slog.Error("failed to send manifest", "err", err)
	}
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
	resp := &pb.SyncResponse{
		Payload: &pb.SyncResponse_Capabilities{
			Capabilities: &pb.PeerCapabilities{
				SupportedCodecs:   []string{"mp3", "flac", "ogg", "wav", "aac"},
				SupportedBitrates: []int32{128, 192, 256, 320},
				ProtocolVersion:   "1.0.0",
			},
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
