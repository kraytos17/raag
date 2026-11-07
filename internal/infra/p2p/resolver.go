package p2p

import (
	"cmp"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

type PeerManager interface {
	FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []peer.ID
	GetPeerScore(pid peer.ID) *domain.PeerScore
	RecordSuccess(pid peer.ID)
	RecordFailure(pid peer.ID)
	IsBanned(pid peer.ID) bool
	GetPeerCapabilities(pid peer.ID) *domain.PeerCapabilities
}

type P2PResolver struct {
	libraryRepo app.LibraryRepository
	pool        *StreamPool
	peerMgr     PeerManager
	scorer      *PeerScorer
	host        StreamOpener
	lastPeerID  peer.ID
	lastPeerMu  sync.RWMutex
}

func NewP2PResolver(libraryRepo app.LibraryRepository, pool *StreamPool, peerMgr PeerManager, scorer *PeerScorer, host StreamOpener) *P2PResolver {
	return &P2PResolver{
		libraryRepo: libraryRepo,
		pool:        pool,
		peerMgr:     peerMgr,
		scorer:      scorer,
		host:        host,
	}
}

func (r *P2PResolver) LastPeerID() peer.ID {
	r.lastPeerMu.RLock()
	defer r.lastPeerMu.RUnlock()
	return r.lastPeerID
}

func (r *P2PResolver) LastPeerIDString() string {
	r.lastPeerMu.RLock()
	defer r.lastPeerMu.RUnlock()
	return r.lastPeerID.String()
}

// fetchTrackMetadata requests a track's metadata from a specific peer over the
// sync protocol.
func (r *P2PResolver) fetchTrackMetadata(ctx context.Context, trackID domain.TrackID, pid peer.ID) (*domain.Track, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	stream, err := r.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return nil, err
	}

	defer stream.Close()
	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_TrackDetailRequest{
			TrackDetailRequest: &pb.TrackDetailRequest{
				TrackId: string(trackID),
			},
		},
	}
	if err := wire.WriteMsg(stream, req); err != nil {
		return nil, err
	}

	var resp pb.SyncResponse
	if err := wire.ReadMsg(stream, &resp); err != nil {
		return nil, err
	}
	if tr, ok := resp.GetPayload().(*pb.SyncResponse_Track); ok {
		return convert.ProtoToTrack(tr.Track), nil
	}
	if er, ok := resp.GetPayload().(*pb.SyncResponse_Error); ok {
		return nil, &SyncError{Code: er.Error.Code, Message: er.Error.Message}
	}
	return nil, errors.New("unexpected response type")
}

func (r *P2PResolver) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []string {
	pids := r.peerMgr.FindPeersWithTrack(ctx, trackID)
	result := make([]string, len(pids))
	for i, p := range pids {
		result[i] = p.String()
	}
	return result
}

func (r *P2PResolver) GetPeerLatency(pid peer.ID) time.Duration {
	if r.scorer == nil {
		return 0
	}
	return r.scorer.AvgLatency(pid)
}

func (r *P2PResolver) Resolve(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error) {
	track, err := r.libraryRepo.FindByID(ctx, trackID)
	if err == nil {
		return r.resolveLocal(track)
	}

	slog.Debug("track not local, trying P2P", "track_id", trackID)
	return r.resolveRemote(ctx, trackID)
}

func (r *P2PResolver) resolveLocal(track *domain.Track) (io.ReadCloser, error) {
	f, err := os.Open(track.Path)
	if err != nil {
		return nil, err
	}

	slog.Debug("resolved track locally", "track_id", track.ID, "path", track.Path)
	return f, nil
}

func (r *P2PResolver) resolveRemote(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error) {
	peers := r.peerMgr.FindPeersWithTrack(ctx, trackID)
	if len(peers) == 0 {
		return nil, domain.ErrTrackNotFound
	}

	scoredPeers := r.scorePeers(ctx, peers)
	if len(scoredPeers) == 0 {
		return nil, domain.ErrTrackNotFound
	}

	nativeCodec := r.nativeCodec(ctx, trackID, scoredPeers)
	return r.tryPeers(ctx, trackID, scoredPeers, nativeCodec)
}

// nativeCodec fetches the remote track's codec once from the best-scored peer
// so tryPeers can decide whether to request transcoding. Best-effort: any
// failure leaves the codec unknown ("") and tryPeers falls back to raw streaming.
func (r *P2PResolver) nativeCodec(ctx context.Context, trackID domain.TrackID, scoredPeers []scoredPeer) string {
	if len(scoredPeers) == 0 {
		return ""
	}

	track, err := r.fetchTrackMetadata(ctx, trackID, scoredPeers[0].pid)
	if err != nil || track == nil {
		return ""
	}
	return track.Codec
}

type scoredPeer struct {
	pid          peer.ID
	score        float64
	bandwidth    int64
	codecs       []string
	canTranscode bool
}

func (r *P2PResolver) scorePeers(ctx context.Context, peers []peer.ID) []scoredPeer {
	scored := make([]scoredPeer, 0, len(peers))
	for _, pid := range peers {
		if r.peerMgr.IsBanned(pid) {
			slog.Debug("skipping banned peer", "peer", pid)
			continue
		}

		peerScore := r.peerMgr.GetPeerScore(pid)
		caps := r.peerMgr.GetPeerCapabilities(pid)
		score := scoredPeer{
			pid:          pid,
			score:        0,
			bandwidth:    0,
			codecs:       caps.SupportedCodecs,
			canTranscode: caps.CanTranscode,
		}
		if peerScore != nil {
			score.score = peerScore.Score()
			score.bandwidth = peerScore.AvgBandwidth
		}
		scored = append(scored, score)
	}

	slices.SortFunc(scored, func(a, b scoredPeer) int {
		return cmp.Compare(b.score, a.score)
	})
	return scored
}

func (r *P2PResolver) tryPeers(ctx context.Context, trackID domain.TrackID, scoredPeers []scoredPeer, nativeCodec string) (io.ReadCloser, error) {
	var lastErr error
	for _, sp := range scoredPeers {
		if r.peerMgr.IsBanned(sp.pid) {
			continue
		}

		codec := requestedCodecFor(nativeCodec, sp)
		client := NewStreamClient(sp.pid, r.pool, r.scorer)
		reader, err := client.GetTrack(ctx, string(trackID), codec, 0)
		if err != nil {
			lastErr = err
			r.peerMgr.RecordFailure(sp.pid)
			slog.Warn("stream failed, trying next peer", "peer", sp.pid, "track_id", trackID, "err", err)
			continue
		}

		slog.Info("streaming track from peer", "track", trackID, "peer", sp.pid, "score", sp.score, "codec", codec)
		return &streamingReader{
			reader:  reader,
			trackID: trackID,
			peerID:  sp.pid,
			onFirstBytes: func() {
				r.peerMgr.RecordSuccess(sp.pid)
				r.lastPeerMu.Lock()
				r.lastPeerID = sp.pid
				r.lastPeerMu.Unlock()
			},
			onFirstError: func(error) {
				r.peerMgr.RecordFailure(sp.pid)
			},
		}, nil
	}
	if lastErr == nil {
		// Every scored peer was banned between scoring and this loop (or there
		// were none left), so no peer was actually tried — return a real error
		// instead of a (nil, nil) that would panic/confuse callers.
		lastErr = errors.New("p2p: no usable peer for track " + string(trackID))
	}
	return nil, lastErr
}

// requestedCodecFor picks the codec to request from a peer. A codec that is
// locally decodable is streamed raw (""). Otherwise, if the peer can transcode,
// request mp3 — libmp3lame output the local engine always decodes. An unknown
// native codec with a transcoding peer is also safe to request as mp3.
func requestedCodecFor(nativeCodec string, sp scoredPeer) string {
	if isLocallyDecodable(nativeCodec) {
		return ""
	}
	if sp.canTranscode {
		return codecMP3
	}
	return ""
}

// isLocallyDecodable reports whether the local engine's decode() can handle the
// given codec, mirroring codecFromExtension's outputs and the engine's decode
// switch. Delegates to the domain-level helper so the set stays in one place.
func isLocallyDecodable(codec string) bool {
	return domain.CodecDecodable(codec)
}

type streamingReader struct {
	reader       io.ReadCloser
	trackID      domain.TrackID
	peerID       peer.ID
	mu           sync.Mutex
	closed       bool
	firstRead    sync.Once
	onFirstBytes func()
	onFirstError func(error)
}

func (r *streamingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	switch {
	case n > 0:
		r.firstRead.Do(func() {
			if r.onFirstBytes != nil {
				r.onFirstBytes()
			}
		})
	case err != nil && !errors.Is(err, context.Canceled):
		r.firstRead.Do(func() {
			if r.onFirstError != nil {
				r.onFirstError(err)
			}
		})
	}
	return n, err
}

// SetReadDeadline forwards to the underlying chunkedReader so the streaming
// fill goroutine's deadline actually bounds reads from a stalled peer.
func (r *streamingReader) SetReadDeadline(t time.Time) error {
	if dr, ok := r.reader.(interface {
		SetReadDeadline(time.Time) error
	}); ok {
		return dr.SetReadDeadline(t)
	}
	return errors.New("streaming reader: underlying source does not support read deadlines")
}

// Deadline forwards to the underlying chunkedReader.
func (r *streamingReader) Deadline() time.Time {
	if dr, ok := r.reader.(interface{ Deadline() time.Time }); ok {
		return dr.Deadline()
	}
	return time.Time{}
}

// Seek forwards the seek to the underlying chunkedReader, repositioning the
// remote stream to the requested byte offset.
func (r *streamingReader) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := r.reader.(io.Seeker)
	if !ok {
		return 0, errors.New("streaming reader: underlying source is not seekable")
	}
	return seeker.Seek(offset, whence)
}

func (r *streamingReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}

	r.closed = true
	return r.reader.Close()
}
