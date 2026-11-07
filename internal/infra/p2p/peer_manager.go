package p2p

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/backoff"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	"github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

type peerManager struct {
	host      host.Host
	peerCache *discovery.PeerCache
	library   app.LibraryRepository
	scorer    *PeerScorer
	repo      app.PeerRepository
	mu        sync.RWMutex
	manifests map[peer.ID]*peerManifest
	caps      map[peer.ID]*peerCaps

	fetchCancelMu sync.RWMutex
	fetchCancel   map[peer.ID]*fetchEntry
	ttl           time.Duration
}

type fetchEntry struct {
	cancel context.CancelFunc
}

type peerManifest struct {
	data    *pb.LibraryManifest
	addedAt time.Time
}

type peerCaps struct {
	data    *pb.PeerCapabilities
	addedAt time.Time
}

func newPeerManager(h host.Host, peerCache *discovery.PeerCache, library app.LibraryRepository, scorer *PeerScorer, ttl time.Duration, repo app.PeerRepository) *peerManager {
	return &peerManager{
		host:        h,
		peerCache:   peerCache,
		library:     library,
		scorer:      scorer,
		repo:        repo,
		manifests:   make(map[peer.ID]*peerManifest),
		caps:        make(map[peer.ID]*peerCaps),
		fetchCancel: make(map[peer.ID]*fetchEntry),
		ttl:         ttl,
	}
}

// persist writes the peer's current live state to the peer repository so that
// IPC consumers (`raag peers`, the TUI panel) see real data instead of an
// empty list. It is called on connect, after data fetch, on score changes,
// and on disconnect. Best-effort: failures are logged, never fatal.
func (pm *peerManager) persist(pid peer.ID) {
	if pm.repo == nil {
		return
	}

	var addrs []string
	if pm.host != nil {
		pi := pm.host.Peerstore().PeerInfo(pid)
		addrs = make([]string, 0, len(pi.Addrs))
		for _, a := range pi.Addrs {
			addrs = append(addrs, a.String())
		}
	}

	info := domain.NewPeerInfo(pid, addrs)
	if caps := pm.GetPeerCapabilities(pid); caps != nil {
		info.Capabilities = caps
	}
	if score := pm.GetPeerScore(context.Background(), pid); score != nil {
		info.Score = score
	}
	if manifest := pm.GetManifest(pid); manifest != nil {
		info.LibrarySummary = convert.ProtoToLibraryManifest(manifest)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := pm.repo.SavePeerInfo(ctx, info); err != nil {
		slog.Debug("failed to persist peer info", "peer", pid, "err", err)
	}
	if info.Score != nil {
		if err := pm.repo.SavePeerScore(ctx, pid, info.Score); err != nil {
			slog.Debug("failed to persist peer score", "peer", pid, "err", err)
		}
	}
	if info.LibrarySummary != nil {
		if err := pm.repo.SaveLibraryManifest(ctx, pid, info.LibrarySummary); err != nil {
			slog.Debug("failed to persist peer manifest", "peer", pid, "err", err)
		}
	}
}

func (pm *peerManager) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []peer.ID {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.pruneExpiredManifestsLocked(time.Now())
	if len(pm.manifests) == 0 {
		slog.Warn("no peer manifests available - privacy settings may prevent sharing")
		return nil
	}

	var owners []peer.ID
	for pid, pmf := range pm.manifests {
		if slices.Contains(pmf.data.TrackIds, string(trackID)) {
			owners = append(owners, pid)
		}
	}
	return owners
}

func (pm *peerManager) GetPeerScore(ctx context.Context, pid peer.ID) *domain.PeerScore {
	if pm.scorer == nil {
		return nil
	}

	lat, avgBW, successes, failures, _ := pm.scorer.Snapshot(pid)
	s := domain.NewPeerScore(pid)
	s.AvgLatency = lat
	s.AvgBandwidth = avgBW
	s.SuccessCount = successes
	s.FailureCount = failures

	return s
}

func (pm *peerManager) RecordFailure(pid peer.ID) {
	if pm.scorer != nil {
		pm.scorer.RecordFailure(pid)
	}
	pm.persist(pid)
}

func (pm *peerManager) RecordSuccess(pid peer.ID) {
	if pm.scorer != nil {
		pm.scorer.RecordSuccess(pid)
	}
	pm.persist(pid)
}

func (pm *peerManager) IsBanned(pid peer.ID) bool {
	if pm.scorer != nil {
		return pm.scorer.IsBanned(pid)
	}
	return false
}

func (pm *peerManager) GetPeerCapabilities(pid peer.ID) *domain.PeerCapabilities {
	if pc := pm.cachedCapabilities(pid); pc != nil {
		return &domain.PeerCapabilities{
			SupportedCodecs:   slices.Clone(pc.data.SupportedCodecs),
			SupportedBitrates: slices.Clone(pc.data.SupportedBitrates),
			CanTranscode:      pc.data.CanTranscode,
			UploadBandwidth:   pc.data.UploadBandwidth,
			ProtocolVersion:   pc.data.ProtocolVersion,
		}
	}
	return &domain.PeerCapabilities{
		SupportedCodecs: domain.PlayableCodecs,
	}
}

func resetStream(s network.Stream) {
	if err := s.Reset(); err != nil {
		slog.Debug("stream reset error", "err", err)
	}
}

func closeStream(s network.Stream) {
	if err := s.Close(); err != nil {
		slog.Debug("stream close error", "err", err)
	}
}

func (pm *peerManager) FetchManifest(ctx context.Context, pid peer.ID) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	stream, err := pm.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return err
	}

	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_ManifestRequest{},
	}
	if err := wire.WriteMsg(stream, req); err != nil {
		resetStream(stream)
		return err
	}

	var resp pb.SyncResponse
	if err := wire.ReadMsg(stream, &resp); err != nil {
		resetStream(stream)
		return err
	}

	switch payload := resp.GetPayload().(type) {
	case *pb.SyncResponse_Manifest:
		pm.mu.Lock()
		pm.manifests[pid] = &peerManifest{data: payload.Manifest, addedAt: time.Now()}
		pm.mu.Unlock()
		closeStream(stream)
		return nil
	case *pb.SyncResponse_Error:
		resetStream(stream)
		return errors.New(payload.Error.Message)
	default:
		resetStream(stream)
		return errors.New("unexpected sync response")
	}
}

func (pm *peerManager) FetchCapabilities(ctx context.Context, pid peer.ID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	stream, err := pm.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return err
	}

	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_CapabilitiesRequest{},
	}
	if err := wire.WriteMsg(stream, req); err != nil {
		resetStream(stream)
		return err
	}

	var resp pb.SyncResponse
	if err := wire.ReadMsg(stream, &resp); err != nil {
		resetStream(stream)
		return err
	}

	switch payload := resp.GetPayload().(type) {
	case *pb.SyncResponse_Capabilities:
		pm.mu.Lock()
		pm.caps[pid] = &peerCaps{data: payload.Capabilities, addedAt: time.Now()}
		pm.mu.Unlock()
		slog.Debug("capabilities received", "peer", pid,
			"codecs", payload.Capabilities.SupportedCodecs,
			"transcode", payload.Capabilities.CanTranscode)
		closeStream(stream)
		return nil
	case *pb.SyncResponse_Error:
		resetStream(stream)
		return errors.New(payload.Error.Message)
	default:
		resetStream(stream)
		return errors.New("unexpected sync response")
	}
}

func (pm *peerManager) GetManifest(pid peer.ID) *pb.LibraryManifest {
	if pmf := pm.cachedManifest(pid); pmf != nil {
		return proto.Clone(pmf.data).(*pb.LibraryManifest)
	}
	return nil
}

func (pm *peerManager) GetCapabilities(pid peer.ID) *pb.PeerCapabilities {
	if pc := pm.cachedCapabilities(pid); pc != nil {
		return proto.Clone(pc.data).(*pb.PeerCapabilities)
	}
	return nil
}

func (pm *peerManager) cachedManifest(pid peer.ID) *peerManifest {
	pm.mu.RLock()
	entry, ok := pm.manifests[pid]
	if !ok || !pm.isExpired(entry.addedAt, time.Now()) {
		pm.mu.RUnlock()
		return entry
	}

	pm.mu.RUnlock()
	pm.mu.Lock()
	if current, exists := pm.manifests[pid]; exists && current == entry && pm.isExpired(current.addedAt, time.Now()) {
		delete(pm.manifests, pid)
	}

	pm.mu.Unlock()
	return nil
}

func (pm *peerManager) cachedCapabilities(pid peer.ID) *peerCaps {
	pm.mu.RLock()
	entry, ok := pm.caps[pid]
	if !ok || !pm.isExpired(entry.addedAt, time.Now()) {
		pm.mu.RUnlock()
		return entry
	}

	pm.mu.RUnlock()
	pm.mu.Lock()
	if current, exists := pm.caps[pid]; exists && current == entry && pm.isExpired(current.addedAt, time.Now()) {
		delete(pm.caps, pid)
	}

	pm.mu.Unlock()
	return nil
}

func (pm *peerManager) isExpired(addedAt, now time.Time) bool {
	return pm.ttl <= 0 || now.Sub(addedAt) > pm.ttl
}

func (pm *peerManager) pruneExpiredManifestsLocked(now time.Time) {
	for pid, manifest := range pm.manifests {
		if pm.isExpired(manifest.addedAt, now) {
			delete(pm.manifests, pid)
		}
	}
}

func (pm *peerManager) FetchPeerData(ctx context.Context, pid peer.ID) {
	pm.fetchCancelMu.Lock()
	if existing, ok := pm.fetchCancel[pid]; ok {
		existing.cancel()
	}

	baseCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	entry := &fetchEntry{cancel: cancel}
	pm.fetchCancel[pid] = entry
	pm.fetchCancelMu.Unlock()

	var trackCount int
	var wg sync.WaitGroup
	wg.Go(func() {
		for attempt := range 4 {
			err := pm.FetchManifest(baseCtx, pid)
			if err == nil {
				manifest := pm.GetManifest(pid)
				if manifest != nil {
					trackCount = len(manifest.TrackIds)
					slog.Info("manifest received", "peer", pid, "tracks", trackCount)
				}
				return
			}
			if err.Error() == "library sharing disabled" {
				slog.Debug("manifest not available", "peer", pid)
				return
			}
			if err.Error() != "permission denied" {
				slog.Warn("failed to fetch manifest", "peer", pid, "err", err)
				return
			}
			if backoff.Wait(baseCtx.Done(), backoff.Linear(attempt+1, 100*time.Millisecond)) {
				return
			}
		}
	})
	wg.Go(func() {
		for attempt := range 4 {
			err := pm.FetchCapabilities(baseCtx, pid)
			if err == nil {
				return
			}
			if err.Error() != "permission denied" {
				slog.Warn("failed to fetch capabilities", "peer", pid, "err", err)
				return
			}
			if backoff.Wait(baseCtx.Done(), backoff.Linear(attempt+1, 100*time.Millisecond)) {
				return
			}
		}
	})

	wg.Wait()
	pm.fetchCancelMu.Lock()
	if current, ok := pm.fetchCancel[pid]; ok && current == entry {
		delete(pm.fetchCancel, pid)
	}
	pm.fetchCancelMu.Unlock()

	if caps := pm.GetCapabilities(pid); caps != nil {
		slog.Info("peer connected with capabilities",
			"peer", pid,
			"tracks", trackCount,
			"codecs", caps.SupportedCodecs,
			"transcode", caps.CanTranscode)
	}
	// Persist the freshly fetched manifest, capabilities, and score so the
	// peer repo reflects them.
	pm.persist(pid)
}

func (pm *peerManager) onPeerDisconnected(pid peer.ID) {
	pm.fetchCancelMu.Lock()
	if entry, ok := pm.fetchCancel[pid]; ok {
		entry.cancel()
		delete(pm.fetchCancel, pid)
	}

	pm.fetchCancelMu.Unlock()
	pm.mu.Lock()
	defer pm.mu.Unlock()

	delete(pm.manifests, pid)
	delete(pm.caps, pid)
	pm.peerCache.Remove(pid)
	if pm.scorer != nil {
		pm.scorer.ClearLatency(pid)
	}
}

func (pm *peerManager) GetBestCodec(pid peer.ID, preferredCodec string) string {
	if pc := pm.cachedCapabilities(pid); pc != nil {
		for _, codec := range []string{preferredCodec, codecOpus, codecMP3, codecFlac} {
			if slices.Contains(pc.data.SupportedCodecs, codec) {
				return codec
			}
		}
	}
	return codecMP3
}

func (pm *peerManager) HasTrack(pid peer.ID, trackID string) bool {
	if pmf := pm.cachedManifest(pid); pmf != nil {
		return slices.Contains(pmf.data.TrackIds, trackID)
	}
	return false
}
