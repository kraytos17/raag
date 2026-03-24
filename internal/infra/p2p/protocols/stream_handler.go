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
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

const MaxChunkSize = 256 * 1024

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
	defer h.mu.Unlock()
	h.allowed[peerID] = true
}

func (h *StreamHandler) Deny(peerID peer.ID) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.allowed, peerID)
}

func (h *StreamHandler) Handle(stream network.Stream) {
	defer stream.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream.SetReadDeadline(time.Now().Add(30 * time.Second))
	var req pb.ChunkRequest
	if err := wire.ReadMsg(stream, &req); err != nil {
		slog.Error("failed to read chunk request", "err", err)
		stream.Reset()
		return
	}
	
	stream.SetReadDeadline(time.Time{})
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
	resp := &pb.ChunkResponse{
		TrackId:   trackID,
		Error:     msg,
		LastChunk: true,
	}

	_ = wire.WriteMsg(stream, resp)
	stream.Reset()
}
