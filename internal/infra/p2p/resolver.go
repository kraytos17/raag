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
	GetPeerScore(ctx context.Context, pid peer.ID) *domain.PeerScore
	RecordSuccess(pid peer.ID)
	RecordFailure(pid peer.ID)
	IsBanned(pid peer.ID) bool
	GetPeerCapabilities(pid peer.ID) *domain.PeerCapabilities
	GetTrackOwners(trackID string) []peer.ID
	GetPeerLatency(pid peer.ID) time.Duration
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
	return nil, errors.New("unexpected response type")
}

func (r *P2PResolver) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []string {
	pids := r.peerMgr.GetTrackOwners(string(trackID))
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

		peerScore := r.peerMgr.GetPeerScore(ctx, pid)
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

		r.peerMgr.RecordSuccess(sp.pid)
		r.lastPeerMu.Lock()
		r.lastPeerID = sp.pid
		r.lastPeerMu.Unlock()

		slog.Info("streaming track from peer", "track", trackID, "peer", sp.pid, "score", sp.score, "codec", codec)
		return &streamingReader{
			reader:  reader,
			trackID: trackID,
			peerID:  sp.pid,
		}, nil
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
// given codec, mirroring codecFromExtension's outputs and the engine's decode switch.
func isLocallyDecodable(codec string) bool {
	switch codec {
	case codecMP3, codecFlac, codecVorbis, codecPCM:
		return true
	default:
		return false
	}
}

type streamingReader struct {
	reader  io.ReadCloser
	trackID domain.TrackID
	peerID  peer.ID
	mu      sync.Mutex
	closed  bool
}

func (r *streamingReader) Read(p []byte) (int, error) {
	return r.reader.Read(p)
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

type PeerScorer struct {
	mu          sync.RWMutex
	failures    map[peer.ID]int
	successes   map[peer.ID]int
	failTotals  map[peer.ID]int
	bandwidths  map[peer.ID][]int64
	lastFailure map[peer.ID]time.Time
	banned      map[peer.ID]bool
	latencies   map[peer.ID][]time.Duration
	failLimit   int
	cooldown    time.Duration
	node        *P2PNode
}

func NewPeerScorer() *PeerScorer {
	return NewPeerScorerWithConfig(3, 5*time.Minute)
}

func NewPeerScorerWithConfig(failLimit int, cooldown time.Duration) *PeerScorer {
	return &PeerScorer{
		failures:    make(map[peer.ID]int),
		successes:   make(map[peer.ID]int),
		failTotals:  make(map[peer.ID]int),
		bandwidths:  make(map[peer.ID][]int64),
		lastFailure: make(map[peer.ID]time.Time),
		banned:      make(map[peer.ID]bool),
		latencies:   make(map[peer.ID][]time.Duration),
		failLimit:   failLimit,
		cooldown:    cooldown,
	}
}

func (s *PeerScorer) RecordLatency(pid peer.ID, rtt time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.latencies[pid] = append(s.latencies[pid], rtt)
	if len(s.latencies[pid]) > 5 {
		s.latencies[pid] = s.latencies[pid][1:]
	}
}

func (s *PeerScorer) AvgLatency(pid peer.ID) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	lats := s.latencies[pid]
	if len(lats) == 0 {
		return 0
	}

	var sum time.Duration
	for _, l := range lats {
		sum += l
	}
	return sum / time.Duration(len(lats))
}

func (s *PeerScorer) RecordBandwidth(pid peer.ID, bytesPerSecond int64) {
	if bytesPerSecond <= 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.bandwidths[pid] = append(s.bandwidths[pid], bytesPerSecond)
	if len(s.bandwidths[pid]) > 5 {
		s.bandwidths[pid] = s.bandwidths[pid][1:]
	}
}

func (s *PeerScorer) AvgBandwidth(pid peer.ID) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	bws := s.bandwidths[pid]
	if len(bws) == 0 {
		return 0
	}

	var sum int64
	for _, bw := range bws {
		sum += bw
	}
	return sum / int64(len(bws))
}

func (s *PeerScorer) RecordSuccess(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.successes[pid]++
	delete(s.failures, pid)
	delete(s.lastFailure, pid)
	delete(s.banned, pid)
	slog.Debug("peer success recorded", "peer", pid)
}

func (s *PeerScorer) RecordFailure(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failTotals[pid]++
	s.failures[pid]++
	s.lastFailure[pid] = time.Now()
	if s.failures[pid] >= s.failLimit {
		s.banned[pid] = true
		slog.Warn("peer banned due to failures", "peer", pid, "failures", s.failures[pid])
		if s.node != nil {
			s.node.BanPeer(pid)
		}
	} else {
		slog.Debug("peer failure recorded", "peer", pid, "failures", s.failures[pid])
	}
}

func (s *PeerScorer) Snapshot(pid peer.ID) (avgLatency time.Duration, avgBandwidth int64, successes int, failures int, banned bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	lats := s.latencies[pid]
	if len(lats) > 0 {
		var sum time.Duration
		for _, l := range lats {
			sum += l
		}
		avgLatency = sum / time.Duration(len(lats))
	}

	successes = s.successes[pid]
	failures = s.failTotals[pid]
	bws := s.bandwidths[pid]
	if len(bws) > 0 {
		var sum int64
		for _, bw := range bws {
			sum += bw
		}
		avgBandwidth = sum / int64(len(bws))
	}
	if s.banned[pid] {
		if time.Since(s.lastFailure[pid]) > s.cooldown {
			delete(s.banned, pid)
			banned = false
		} else {
			banned = true
		}
	}
	return avgLatency, avgBandwidth, successes, failures, banned
}

func (s *PeerScorer) ClearLatency(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.latencies, pid)
}

func (s *PeerScorer) IsBanned(pid peer.ID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.banned[pid] {
		return false
	}
	if time.Since(s.lastFailure[pid]) > s.cooldown {
		delete(s.banned, pid)
		return false
	}
	return true
}

func (s *PeerScorer) Reset(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.failures, pid)
	delete(s.successes, pid)
	delete(s.failTotals, pid)
	delete(s.bandwidths, pid)
	delete(s.lastFailure, pid)
	delete(s.banned, pid)
	delete(s.latencies, pid)
}

func (s *PeerScorer) Score(pid peer.ID, latency time.Duration, bandwidth int64, successRate float64) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	latencyScore := 1.0 / (1.0 + latency.Seconds())
	bandwidthScore := float64(bandwidth) / 1e6 * 0.1
	return latencyScore*0.5 + successRate*0.4 + bandwidthScore*0.1
}

type P2PResolverAdapter struct {
	resolver      *P2PResolver
	peersProvider func() []peer.ID
}

func NewP2PResolverAdapter(resolver *P2PResolver) *P2PResolverAdapter {
	return &P2PResolverAdapter{resolver: resolver}
}

// SetPeersProvider wires a function that returns currently connected peers
// (e.g. the P2P node's Peers). Required for SearchRemote fan-out.
func (a *P2PResolverAdapter) SetPeersProvider(fn func() []peer.ID) {
	a.peersProvider = fn
}

func (a *P2PResolverAdapter) Resolve(ctx context.Context, trackID domain.TrackID) (io.ReadCloser, error) {
	return a.resolver.Resolve(ctx, trackID)
}

func (a *P2PResolverAdapter) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []string {
	return a.resolver.FindPeersWithTrack(ctx, trackID)
}

func (a *P2PResolverAdapter) LastPeerID() string {
	return a.resolver.LastPeerIDString()
}

func (a *P2PResolverAdapter) FetchTrackMetadata(ctx context.Context, trackID domain.TrackID, peerID string) (*domain.Track, error) {
	pid, err := peer.Decode(peerID)
	if err != nil {
		return nil, err
	}
	return a.resolver.fetchTrackMetadata(ctx, trackID, pid)
}

// SearchPeer asks a single peer to search its own library and returns the
// matching tracks (with metadata). Best-effort: any error propagates so the
// caller can skip the peer.
func (a *P2PResolverAdapter) SearchPeer(ctx context.Context, pid peer.ID, query string, limit int) ([]*domain.Track, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	stream, err := a.resolver.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_RemoteSearchRequest{
			RemoteSearchRequest: &pb.RemoteSearchRequest{
				Query: query,
				Limit: int32(limit),
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
	if sr, ok := resp.GetPayload().(*pb.SyncResponse_Search); ok {
		tracks := make([]*domain.Track, 0, len(sr.Search.Tracks))
		for _, t := range sr.Search.Tracks {
			tracks = append(tracks, convert.ProtoToTrack(t))
		}
		return tracks, nil
	}
	if er, ok := resp.GetPayload().(*pb.SyncResponse_Error); ok {
		return nil, errors.New(er.Error.Message)
	}
	return nil, errors.New("unexpected response type")
}

// Peers returns the currently connected peer IDs.
func (a *P2PResolverAdapter) Peers() []peer.ID {
	if a.peersProvider != nil {
		return a.peersProvider()
	}
	return nil
}
