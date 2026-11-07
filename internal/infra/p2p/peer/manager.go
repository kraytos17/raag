package peer

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/backoff"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

type Manager struct {
	host      host.Host
	peerCache *discovery.PeerCache
	library   app.LibraryRepository
	scorer    *Scorer
	repo      app.PeerRepository
	fetcher   *peerFetcher
	mu        sync.RWMutex
	manifests map[peer.ID]*peerManifest
	caps      map[peer.ID]*peerCaps

	ttl time.Duration
}

// PeerManager is the peer-query/scoring surface used by the resolver.
type PeerManager interface {
	FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []peer.ID
	GetPeerScore(pid peer.ID) *domain.PeerScore
	RecordSuccess(pid peer.ID)
	RecordFailure(pid peer.ID)
	IsBanned(pid peer.ID) bool
	GetPeerCapabilities(pid peer.ID) *domain.PeerCapabilities
}

// Lifecycle is the extra peer state operations owned by a node. The
// concrete manager and its caches remain private to this package.
type Lifecycle interface {
	PeerManager
	GetPeerCapabilities(pid peer.ID) *domain.PeerCapabilities
	GetManifest(pid peer.ID) *pb.LibraryManifest
	FetchPeerData(ctx context.Context, pid peer.ID)
	CachePeer(pi peer.AddrInfo)
	Persist(pid peer.ID)
	OnPeerDisconnected(pid peer.ID)
}

type peerManifest struct {
	data    *pb.LibraryManifest
	addedAt time.Time
}

type peerCaps struct {
	data    *pb.PeerCapabilities
	addedAt time.Time
}

func NewManager(h host.Host, peerCache *discovery.PeerCache, library app.LibraryRepository, scorer *Scorer, ttl time.Duration, repo app.PeerRepository) *Manager {
	return &Manager{
		host:      h,
		peerCache: peerCache,
		library:   library,
		scorer:    scorer,
		repo:      repo,
		fetcher:   newPeerFetcher(h),
		manifests: make(map[peer.ID]*peerManifest),
		caps:      make(map[peer.ID]*peerCaps),
		ttl:       ttl,
	}
}

// persist writes the peer's current live state to the peer repository so that
// IPC consumers (`raag peers`, the TUI panel) see real data instead of an
// empty list. It is called on connect, after data fetch, on score changes,
// and on disconnect. Best-effort: failures are logged, never fatal.
func (pm *Manager) persist(pid peer.ID) {
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
	if score := pm.GetPeerScore(pid); score != nil {
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

func (pm *Manager) Persist(pid peer.ID) { pm.persist(pid) }

func (pm *Manager) CachePeer(pi peer.AddrInfo) { pm.peerCache.Add(pi) }

func (pm *Manager) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []peer.ID {
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

func (pm *Manager) GetPeerScore(pid peer.ID) *domain.PeerScore {
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

func (pm *Manager) RecordFailure(pid peer.ID) {
	if pm.scorer != nil {
		pm.scorer.RecordFailure(pid)
	}
	pm.persist(pid)
}

func (pm *Manager) RecordSuccess(pid peer.ID) {
	if pm.scorer != nil {
		pm.scorer.RecordSuccess(pid)
	}
	pm.persist(pid)
}

func (pm *Manager) IsBanned(pid peer.ID) bool {
	if pm.scorer != nil {
		return pm.scorer.IsBanned(pid)
	}
	return false
}

func (pm *Manager) GetPeerCapabilities(pid peer.ID) *domain.PeerCapabilities {
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

func (pm *Manager) FetchManifest(ctx context.Context, pid peer.ID) error {
	manifest, err := pm.fetcher.fetchManifest(ctx, pid)
	if err != nil {
		return err
	}
	
	pm.mu.Lock()
	pm.manifests[pid] = &peerManifest{data: manifest, addedAt: time.Now()}
	pm.mu.Unlock()
	return nil
}

func (pm *Manager) FetchCapabilities(ctx context.Context, pid peer.ID) error {
	caps, err := pm.fetcher.fetchCapabilities(ctx, pid)
	if err != nil {
		return err
	}
	
	pm.mu.Lock()
	pm.caps[pid] = &peerCaps{data: caps, addedAt: time.Now()}
	pm.mu.Unlock()
	return nil
}

func (pm *Manager) GetManifest(pid peer.ID) *pb.LibraryManifest {
	if pmf := pm.cachedManifest(pid); pmf != nil {
		return proto.Clone(pmf.data).(*pb.LibraryManifest)
	}
	return nil
}

func (pm *Manager) GetCapabilities(pid peer.ID) *pb.PeerCapabilities {
	if pc := pm.cachedCapabilities(pid); pc != nil {
		return proto.Clone(pc.data).(*pb.PeerCapabilities)
	}
	return nil
}

func (pm *Manager) cachedManifest(pid peer.ID) *peerManifest {
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

func (pm *Manager) cachedCapabilities(pid peer.ID) *peerCaps {
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

func (pm *Manager) isExpired(addedAt, now time.Time) bool {
	return pm.ttl <= 0 || now.Sub(addedAt) > pm.ttl
}

func (pm *Manager) pruneExpiredManifestsLocked(now time.Time) {
	for pid, manifest := range pm.manifests {
		if pm.isExpired(manifest.addedAt, now) {
			delete(pm.manifests, pid)
		}
	}
}

func (pm *Manager) FetchPeerData(ctx context.Context, pid peer.ID) {
	baseCtx, entry := pm.fetcher.start(ctx, pid)

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
			if baseCtx.Err() != nil {
				// Superseded by a newer fetch or torn down by disconnect;
				// the current fetch owns the peer now.
				return
			}
			if code, ok := ErrorCode(err); ok {
				if code == pb.ErrorCode_ERROR_CODE_PERMISSION_DENIED {
					slog.Debug("manifest not available", "peer", pid)
				} else {
					slog.Warn("failed to fetch manifest", "peer", pid, "err", err)
				}
				return
			}

			slog.Warn("failed to fetch manifest", "peer", pid, "err", err)
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
			if baseCtx.Err() != nil {
				return
			}
			if _, ok := ErrorCode(err); ok {
				slog.Warn("failed to fetch capabilities", "peer", pid, "err", err)
				return
			}

			slog.Warn("failed to fetch capabilities", "peer", pid, "err", err)
			if backoff.Wait(baseCtx.Done(), backoff.Linear(attempt+1, 100*time.Millisecond)) {
				return
			}
		}
	})

	wg.Wait()
	pm.fetcher.finish(pid, entry)
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

func (pm *Manager) onPeerDisconnected(pid peer.ID) {
	pm.fetcher.cancelFetch(pid)
	pm.mu.Lock()
	defer pm.mu.Unlock()

	delete(pm.manifests, pid)
	delete(pm.caps, pid)
	pm.peerCache.Remove(pid)
	if pm.scorer != nil {
		pm.scorer.ClearLatency(pid)
	}
}

func (pm *Manager) OnPeerDisconnected(pid peer.ID) { pm.onPeerDisconnected(pid) }

func (pm *Manager) GetBestCodec(pid peer.ID, preferredCodec string) string {
	if pc := pm.cachedCapabilities(pid); pc != nil {
		for _, codec := range []string{preferredCodec, CodecOpus, CodecMP3, CodecFlac} {
			if slices.Contains(pc.data.SupportedCodecs, codec) {
				return codec
			}
		}
	}
	return CodecMP3
}

func (pm *Manager) HasTrack(pid peer.ID, trackID string) bool {
	if pmf := pm.cachedManifest(pid); pmf != nil {
		return slices.Contains(pmf.data.TrackIds, trackID)
	}
	return false
}
