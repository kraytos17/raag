package p2p

import (
	"context"
	"iter"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	pb "github.com/p-society/raag/proto/gen"
)

// memPeerRepo is an in-memory app.PeerRepository for testing persistence.
type memPeerRepo struct {
	mu        sync.Mutex
	infos     map[domain.PeerID]*domain.PeerInfo
	scores    map[domain.PeerID]*domain.PeerScore
	manifests map[domain.PeerID]*domain.LibraryManifest
}

func newMemPeerRepo() *memPeerRepo {
	return &memPeerRepo{
		infos:     make(map[domain.PeerID]*domain.PeerInfo),
		scores:    make(map[domain.PeerID]*domain.PeerScore),
		manifests: make(map[domain.PeerID]*domain.LibraryManifest),
	}
}

func (m *memPeerRepo) SavePeerInfo(_ context.Context, info *domain.PeerInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.infos[info.ID] = info
	return nil
}

func (m *memPeerRepo) GetPeerInfo(_ context.Context, id domain.PeerID) (*domain.PeerInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if info, ok := m.infos[id]; ok {
		return info, nil
	}
	return nil, domain.ErrPeerUnavailable
}

func (m *memPeerRepo) SavePeerScore(_ context.Context, id domain.PeerID, score *domain.PeerScore) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scores[id] = score
	return nil
}

func (m *memPeerRepo) GetPeerScore(_ context.Context, id domain.PeerID) (*domain.PeerScore, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if score, ok := m.scores[id]; ok {
		return score, nil
	}
	return nil, nil
}

func (m *memPeerRepo) SaveLibraryManifest(_ context.Context, id domain.PeerID, manifest *domain.LibraryManifest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.manifests[id] = manifest
	return nil
}

func (m *memPeerRepo) GetLibraryManifest(_ context.Context, id domain.PeerID) (*domain.LibraryManifest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if manifest, ok := m.manifests[id]; ok {
		return manifest, nil
	}
	return nil, nil
}

func (m *memPeerRepo) ListAll(_ context.Context) iter.Seq2[*domain.PeerInfo, error] {
	return func(yield func(*domain.PeerInfo, error) bool) {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, info := range m.infos {
			if !yield(info, nil) {
				return
			}
		}
	}
}

// TestPeerManager_PersistRoundTrip verifies that persist() writes peer info,
// score, and manifest through the repository.
func TestPeerManager_PersistRoundTrip(t *testing.T) {
	repo := newMemPeerRepo()
	pm := newPeerManager(nil, discovery.NewPeerCache(100), nil, nil, time.Hour, repo)

	pid := domain.PeerID("peer-1")
	score := domain.NewPeerScore(pid)
	score.SuccessCount = 5
	score.AvgLatency = 10 * time.Millisecond

	// Seed the scorer with a real score so persist() snapshots it.
	scorer := NewPeerScorer()
	scorer.RecordSuccess(pid)
	scorer.RecordSuccess(pid)
	scorer.RecordBandwidth(pid, 1_000_000)
	pm.scorer = scorer

	// Seed a manifest so persist() stores it too.
	pm.mu.Lock()
	pm.manifests[pid] = &peerManifest{
		data:    &pb.LibraryManifest{TrackIds: []string{"t1", "t2"}},
		addedAt: time.Now(),
	}
	pm.mu.Unlock()

	pm.persist(pid)

	got, err := repo.GetPeerInfo(context.Background(), pid)
	if err != nil {
		t.Fatalf("GetPeerInfo: %v", err)
	}
	if got == nil || got.ID != pid {
		t.Fatalf("expected persisted peer info for %s, got %+v", pid, got)
	}
	if got.Score == nil || got.Score.SuccessCount != 2 {
		t.Fatalf("expected score success_count=2, got %+v", got.Score)
	}

	manifest, err := repo.GetLibraryManifest(context.Background(), pid)
	if err != nil {
		t.Fatalf("GetLibraryManifest: %v", err)
	}
	if manifest == nil || len(manifest.TrackIDs) != 2 {
		t.Fatalf("expected manifest with 2 tracks, got %+v", manifest)
	}

	if _, err := repo.GetPeerScore(context.Background(), pid); err != nil {
		t.Fatalf("GetPeerScore: %v", err)
	}
}

// TestPeerManager_PersistNoRepo verifies persist() is a safe no-op when no
// repository is configured (unit tests / P2P-disabled builds).
func TestPeerManager_PersistNoRepo(t *testing.T) {
	pm := newPeerManager(nil, discovery.NewPeerCache(100), nil, nil, time.Hour, nil)
	pm.persist(domain.PeerID("peer-1")) // must not panic
}

// TestPeerManager_PersistNilScorer verifies persist() handles a nil scorer.
func TestPeerManager_PersistNilScorer(t *testing.T) {
	repo := newMemPeerRepo()
	pm := newPeerManager(nil, discovery.NewPeerCache(100), nil, nil, time.Hour, repo)
	pm.persist(domain.PeerID("peer-1"))

	if _, err := repo.GetPeerInfo(context.Background(), domain.PeerID("peer-1")); err != nil {
		t.Fatalf("expected peer info persisted even with nil scorer: %v", err)
	}
}

func TestPeerManager_ExpiredCacheReadsAreConcurrentSafe(t *testing.T) {
	pid := peer.ID("expired-cache-peer")
	pm := newPeerManager(nil, discovery.NewPeerCache(10), nil, nil, -time.Second, nil)
	pm.manifests[pid] = &peerManifest{
		data:    &pb.LibraryManifest{TrackIds: []string{track1ID}},
		addedAt: time.Now().Add(-time.Hour),
	}
	pm.caps[pid] = &peerCaps{
		data:    &pb.PeerCapabilities{SupportedCodecs: []string{peerTestOpus}},
		addedAt: time.Now().Add(-time.Hour),
	}

	const readers = 64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range readers {
		wg.Go(func() {
			<-start
			if got := pm.GetManifest(pid); got != nil {
				t.Errorf("GetManifest() = %v, want nil for expired entry", got)
			}
			if got := pm.GetCapabilities(pid); got != nil {
				t.Errorf("GetCapabilities() = %v, want nil for expired entry", got)
			}
			if got := pm.GetPeerCapabilities(pid); got == nil {
				t.Error("GetPeerCapabilities() = nil, want playable-codec fallback")
			}
			if pm.HasTrack(pid, track1ID) {
				t.Error("HasTrack() = true, want false for expired entry")
			}
			if got := pm.FindPeersWithTrack(context.Background(), domain.TrackID(track1ID)); len(got) != 0 {
				t.Errorf("FindPeersWithTrack() = %v, want no expired peers", got)
			}
			if got := pm.GetTrackOwners(track1ID); len(got) != 0 {
				t.Errorf("GetTrackOwners() = %v, want no expired peers", got)
			}
			if got := pm.GetBestCodec(pid, peerTestOpus); got != codecMP3 {
				t.Errorf("GetBestCodec() = %q, want fallback %q", got, codecMP3)
			}
		})
	}
	close(start)
	wg.Wait()
}

func TestPeerManager_CacheGettersReturnIndependentSnapshots(t *testing.T) {
	pid := peer.ID("snapshot-cache-peer")
	pm := newPeerManager(nil, discovery.NewPeerCache(10), nil, nil, time.Hour, nil)
	pm.manifests[pid] = &peerManifest{
		data: &pb.LibraryManifest{
			PeerId:   "original-peer",
			TrackIds: []string{track1ID, "track-2"},
		},
		addedAt: time.Now(),
	}
	pm.caps[pid] = &peerCaps{
		data: &pb.PeerCapabilities{
			SupportedCodecs:   []string{peerTestOpus, peerTestMP3},
			SupportedBitrates: []int32{128, 256},
			ProtocolVersion:   peerProtocolV1,
		},
		addedAt: time.Now(),
	}

	manifest := pm.GetManifest(pid)
	manifest.PeerId = "corrupted-peer"
	manifest.TrackIds[0] = "corrupted-track"
	manifest.TrackIds = append(manifest.TrackIds, "injected-track")

	caps := pm.GetCapabilities(pid)
	caps.SupportedCodecs[0] = "corrupted-codec"
	caps.SupportedBitrates[0] = 999
	caps.ProtocolVersion = "corrupted-version"

	domainCaps := pm.GetPeerCapabilities(pid)
	domainCaps.SupportedCodecs[0] = "corrupted-domain-codec"
	domainCaps.SupportedBitrates[0] = 999
	domainCaps.ProtocolVersion = "corrupted-domain-version"

	gotManifest := pm.GetManifest(pid)
	if gotManifest.PeerId != "original-peer" || !slices.Equal(gotManifest.TrackIds, []string{track1ID, "track-2"}) {
		t.Errorf("cached manifest changed through returned snapshot: %+v", gotManifest)
	}
	gotCaps := pm.GetCapabilities(pid)
	if gotCaps.ProtocolVersion != peerProtocolV1 ||
		!slices.Equal(gotCaps.SupportedCodecs, []string{peerTestOpus, peerTestMP3}) ||
		!slices.Equal(gotCaps.SupportedBitrates, []int32{128, 256}) {
		t.Errorf("cached capabilities changed through returned snapshot: %+v", gotCaps)
	}
	gotDomainCaps := pm.GetPeerCapabilities(pid)
	if gotDomainCaps.ProtocolVersion != peerProtocolV1 ||
		!slices.Equal(gotDomainCaps.SupportedCodecs, []string{peerTestOpus, peerTestMP3}) ||
		!slices.Equal(gotDomainCaps.SupportedBitrates, []int32{128, 256}) {
		t.Errorf("cached domain capabilities changed through returned snapshot: %+v", gotDomainCaps)
	}
}

// compile-time check that memPeerRepo satisfies app.PeerRepository.
var _ app.PeerRepository = (*memPeerRepo)(nil)
