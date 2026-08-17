package protocols

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/transcoder"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

const MaxChunkSize = 256 * 1024

type StreamHandler struct {
	mu         sync.RWMutex
	library    app.LibraryRepository
	admission  *AdmissionRegistry
	transcoder *transcoder.Transcoder
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

// SetTranscoder enables on-demand transcoding when a request specifies a
// codec different from the track's native format.
func (h *StreamHandler) SetTranscoder(t *transcoder.Transcoder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.transcoder = t
}

// Transcoder returns the configured transcoder, or nil if none is set.
func (h *StreamHandler) Transcoder() *transcoder.Transcoder {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.transcoder
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
			slog.Warn(
				"chunk request from unadmitted peer",
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

	h.mu.RLock()
	tr := h.transcoder
	h.mu.RUnlock()

	// If a codec is requested and it differs from the track's native codec,
	// transcode to a temp file and serve byte ranges from it.
	if tr != nil && req.Codec != "" && req.Codec != track.Codec {
		h.serveTranscoded(ctx, stream, tr, track, req)
		return
	}
	h.serveRaw(ctx, stream, track, req)
}

func (h *StreamHandler) serveTranscoded(ctx context.Context, stream network.Stream, tr *transcoder.Transcoder, track *domain.Track, req *pb.ChunkRequest) {
	bitrate := ""
	if req.Bitrate > 0 {
		bitrate = fmt.Sprintf("%dk", req.Bitrate)
	} else {
		bitrate = "128k"
	}

	// The transcode runs outside the chunk request's deadline: a long track can
	// take minutes to transcode, and the request ctx is a 30s bound on serving
	// the response, not on producing it. transcodeCtx inside TranscodeToFile
	// still enforces its own cap.
	tmpPath, err := tr.TranscodeToFile(context.WithoutCancel(ctx), track.Path, req.Codec, bitrate)
	if err != nil {
		slog.Error("transcode failed", "track_id", req.TrackId, "codec", req.Codec, "err", err)
		h.sendError(stream, req.TrackId, "transcode error", pb.ErrorCode_ERROR_CODE_TRANSCODE_FAILED)
		return
	}

	f, err := os.Open(tmpPath)
	if err != nil {
		h.sendError(stream, req.TrackId, "transcode output error", pb.ErrorCode_ERROR_CODE_TRANSCODE_FAILED)
		return
	}
	defer func() {
		if err := f.Close(); err != nil {
			slog.Debug("failed to close transcode output", "err", err)
		}
	}()

	var totalSize int64
	if info, err := f.Stat(); err == nil {
		totalSize = info.Size()
	}

	if req.Offset > 0 {
		if _, err := f.Seek(req.Offset, io.SeekStart); err != nil {
			h.sendError(stream, req.TrackId, "seek error", pb.ErrorCode_ERROR_CODE_UNSPECIFIED)
			return
		}
	}

	length := min(int(req.Length), MaxChunkSize)
	data := make([]byte, length)
	n, err := io.ReadFull(f, data)
	if err != nil && err != io.EOF {
		h.sendError(stream, req.TrackId, "read error", pb.ErrorCode_ERROR_CODE_UNSPECIFIED)
		return
	}

	lastChunk := n < int(req.Length) || err == io.EOF
	resp := &pb.ChunkResponse{
		TrackId:   req.TrackId,
		Offset:    req.Offset,
		Data:      data[:n],
		LastChunk: lastChunk,
		TotalSize: totalSize,
		MimeType:  "audio/" + req.Codec,
	}
	if err := wire.WriteMsg(stream, resp); err != nil {
		slog.Error("write response failed", "err", err)
		return
	}
}

func (h *StreamHandler) serveRaw(ctx context.Context, stream network.Stream, track *domain.Track, req *pb.ChunkRequest) {
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
