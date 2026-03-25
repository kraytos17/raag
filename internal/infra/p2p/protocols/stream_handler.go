package protocols

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

const MaxChunkSize = 256 * 1024

type StreamHandler struct {
	mu        sync.RWMutex
	library   app.LibraryRepository
	admission *AdmissionRegistry
}

func NewStreamHandler(library app.LibraryRepository) *StreamHandler {
	return &StreamHandler{
		library: library,
	}
}

func (h *StreamHandler) SetAdmissionRegistry(admission *AdmissionRegistry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.admission = admission
}

func (h *StreamHandler) Handle(stream network.Stream) {
	defer func() {
		if err := stream.Close(); err != nil {
			slog.Debug("stream close error", "err", err)
		}
	}()

	peerID := stream.Conn().RemotePeer()
	for {
		if err := stream.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
			slog.Debug("failed to set read deadline", "err", err)
		}

		var req pb.ChunkRequest
		if err := wire.ReadMsg(stream, &req); err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			slog.Error("failed to read chunk request", "err", err)
			if err := stream.Reset(); err != nil {
				slog.Debug("stream reset error", "err", err)
			}
			return
		}
		if err := stream.SetReadDeadline(time.Time{}); err != nil {
			slog.Debug("failed to clear read deadline", "err", err)
		}
		if h.admission == nil || !h.admission.IsAdmitted(peerID) {
			slog.Warn("chunk request from unadmitted peer",
				"peer", peerID,
				"track", req.TrackId,
			)
			h.sendError(stream, req.TrackId, "manifest exchange required", pb.ErrorCode_ERROR_CODE_UNAUTHORIZED)
			return
		}

		reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		h.serveChunk(reqCtx, stream, &req)
		cancel()
	}
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
	defer func() {
		if err := f.Close(); err != nil {
			slog.Debug("failed to close track file", "path", track.Path, "err", err)
		}
	}()
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
	if err := stream.Reset(); err != nil {
		slog.Debug("stream reset error in sendError", "err", err)
	}
}
