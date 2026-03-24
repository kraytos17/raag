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
	lastPeerID  peer.ID
	host        StreamOpener
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
	return r.lastPeerID
}

func (r *P2PResolver) LastPeerIDString() string {
	return r.lastPeerID.String()
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
	return r.tryPeers(ctx, trackID, scoredPeers)
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

func (r *P2PResolver) tryPeers(ctx context.Context, trackID domain.TrackID, scoredPeers []scoredPeer) (io.ReadCloser, error) {
	var lastErr error
	for _, sp := range scoredPeers {
		if r.peerMgr.IsBanned(sp.pid) {
			continue
		}

		client := NewStreamClient(sp.pid, r.pool)
		reader, err := client.GetTrack(ctx, string(trackID), "", 0)
		if err != nil {
			lastErr = err
			r.peerMgr.RecordFailure(sp.pid)
			slog.Warn("stream failed, trying next peer", "peer", sp.pid, "track_id", trackID, "err", err)
			continue
		}

		r.peerMgr.RecordSuccess(sp.pid)
		r.lastPeerID = sp.pid
		slog.Info("streaming track from peer", "track", trackID, "peer", sp.pid, "score", sp.score)
		return &streamingReader{
			reader:  reader,
			trackID: trackID,
			peerID:  sp.pid,
		}, nil
	}
	return nil, lastErr
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
	lastFailure map[peer.ID]time.Time
	banned      map[peer.ID]bool
	latencies   map[peer.ID][]time.Duration
	failLimit   int
	cooldown    time.Duration
}

func NewPeerScorer() *PeerScorer {
	return NewPeerScorerWithConfig(3, 5*time.Minute)
}

func NewPeerScorerWithConfig(failLimit int, cooldown time.Duration) *PeerScorer {
	return &PeerScorer{
		failures:    make(map[peer.ID]int),
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

func (s *PeerScorer) RecordSuccess(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.failures, pid)
	delete(s.lastFailure, pid)
	delete(s.banned, pid)
}

func (s *PeerScorer) RecordFailure(pid peer.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.failures[pid]++
	s.lastFailure[pid] = time.Now()
	if s.failures[pid] >= s.failLimit {
		s.banned[pid] = true
		slog.Warn("peer banned due to failures", "peer", pid, "failures", s.failures[pid])
	}
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
	delete(s.lastFailure, pid)
	delete(s.banned, pid)
}

func (s *PeerScorer) Score(pid peer.ID, latency time.Duration, bandwidth int64, successRate float64) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	latencyScore := 1.0 / (1.0 + latency.Seconds())
	bandwidthScore := float64(bandwidth) / 1e6 * 0.1
	return latencyScore*0.5 + successRate*0.4 + bandwidthScore*0.1
}

type P2PResolverAdapter struct {
	resolver *P2PResolver
}

func NewP2PResolverAdapter(resolver *P2PResolver) *P2PResolverAdapter {
	return &P2PResolverAdapter{resolver: resolver}
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

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	stream, err := a.resolver.host.NewStream(ctx, pid, protocols.SyncProtocol)
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
