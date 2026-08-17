package p2p

import (
	"context"
	"iter"
	"sync"
	"testing"
	"time"

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

// compile-time check that memPeerRepo satisfies app.PeerRepository.
var _ app.PeerRepository = (*memPeerRepo)(nil)
