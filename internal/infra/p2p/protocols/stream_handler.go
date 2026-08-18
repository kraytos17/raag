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
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/fsroot"
	"github.com/p-society/raag/internal/infra/transcoder"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
	"golang.org/x/time/rate"
)

const MaxChunkSize = 256 * 1024

type StreamHandler struct {
	mu         sync.RWMutex
	library    app.LibraryRepository
	roots      *fsroot.Roots
	admission  *AdmissionRegistry
	transcoder *transcoder.Transcoder

	// reqPerSec caps chunk requests per second per peer (0 = unlimited).
	reqPerSec int
	// uploadBPS caps the aggregate bytes served per second across all peers
	// (0 = unlimited).
	uploadBPS int64
	// reqLimiters is a lazily-populated per-peer request limiter map.
	reqLimitersMu sync.Mutex
	reqLimiters   map[peer.ID]*rate.Limiter
	// uploadLimiter is the single global byte-rate limiter shared by all peers.
	uploadLimiter *rate.Limiter
}

// NewStreamHandler creates a handler that serves track chunks over libp2p
// streams. roots scopes every file open to the configured library directories;
// when nil, serving is disabled for any path that cannot be resolved (the
// safe default for tests and misconfiguration).
func NewStreamHandler(library app.LibraryRepository, roots *fsroot.Roots) *StreamHandler {
	return &StreamHandler{
		library:     library,
		roots:       roots,
		reqLimiters: make(map[peer.ID]*rate.Limiter),
	}
}

func (h *StreamHandler) SetAdmissionRegistry(admission *AdmissionRegistry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.admission = admission
}

// SetRateLimiters configures per-peer request rate limiting (requests/sec) and
// the global upload bandwidth cap (bytes/sec). Values <= 0 disable the
// corresponding limiter, matching the "0 = unlimited" config semantics.
func (h *StreamHandler) SetRateLimiters(reqPerSec int, uploadBPS int64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.reqPerSec = reqPerSec
	h.uploadBPS = uploadBPS
	if reqPerSec > 0 {
		h.reqLimiters = make(map[peer.ID]*rate.Limiter)
	}
	if uploadBPS > 0 {
		h.uploadLimiter = rate.NewLimiter(rate.Limit(uploadBPS), burstBytes(uploadBPS))
	} else {
		h.uploadLimiter = nil
	}
}

// burstBytes returns a sensible burst capacity for a byte-rate limiter: at most
// one second of budget, but at least a full chunk so a single response can
// always begin.
func burstBytes(bps int64) int {
	if bps >= MaxChunkSize {
		return MaxChunkSize
	}
	if bps <= 0 {
		return 1
	}
	return int(bps)
}

// requestLimiter returns the per-peer request limiter, creating it lazily.
// nil means request rate limiting is disabled.
func (h *StreamHandler) requestLimiter(pid peer.ID) *rate.Limiter {
	h.mu.RLock()
	reqPerSec := h.reqPerSec
	h.mu.RUnlock()
	if reqPerSec <= 0 {
		return nil
	}

	h.reqLimitersMu.Lock()
	defer h.reqLimitersMu.Unlock()
	if l, ok := h.reqLimiters[pid]; ok {
		return l
	}

	l := rate.NewLimiter(rate.Limit(reqPerSec), reqPerSec)
	h.reqLimiters[pid] = l
	return l
}

// waitRequest blocks until the peer is allowed to send another chunk request.
func (h *StreamHandler) waitRequest(ctx context.Context, pid peer.ID) error {
	limiter := h.requestLimiter(pid)
	if limiter == nil {
		return nil
	}
	return limiter.Wait(ctx)
}

// waitUpload blocks until the global upload budget has room for n bytes.
func (h *StreamHandler) waitUpload(ctx context.Context, n int64) error {
	h.mu.RLock()
	limiter := h.uploadLimiter
	h.mu.RUnlock()
	if limiter == nil || n <= 0 {
		return nil
	}
	return limiter.WaitN(ctx, int(n))
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

		// Per-peer request rate limit: wait for a token before serving. The
		// wait is bounded by reqCtx so a heavily throttled peer eventually
		// times out instead of holding the handler goroutine forever.
		reqCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if err := h.waitRequest(reqCtx, peerID); err != nil {
			slog.Warn("chunk request throttled or timed out", "peer", peerID, "err", err)
			cancel()
			return
		}
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
	// The transcode runs ffmpeg as a subprocess, which os.Root cannot scope.
	// Reject any path outside a configured library root before handing it over.
	if h.roots == nil || !h.roots.Contains(track.Path) {
		slog.Warn("refusing to transcode path outside library roots", "path", track.Path)
		h.sendError(stream, req.TrackId, "transcode error", pb.ErrorCode_ERROR_CODE_TRANSCODE_FAILED)
		return
	}

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
	if err := h.waitUpload(ctx, int64(n)); err != nil {
		slog.Warn("upload throttled or timed out", "track_id", req.TrackId, "err", err)
		return
	}
	if err := wire.WriteMsg(stream, resp); err != nil {
		slog.Error("write response failed", "err", err)
		return
	}
}

func (h *StreamHandler) serveRaw(ctx context.Context, stream network.Stream, track *domain.Track, req *pb.ChunkRequest) {
	if h.roots == nil {
		h.sendError(stream, req.TrackId, "library serving disabled", pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED)
		return
	}

	f, err := h.roots.Open(track.Path)
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
	if err := h.waitUpload(ctx, int64(n)); err != nil {
		slog.Warn("upload throttled or timed out", "track_id", req.TrackId, "err", err)
		return
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
