package p2p

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/domain"
)

type mockPeerManager struct {
	peers       map[peer.ID]*domain.PeerInfo
	scores      map[peer.ID]*domain.PeerScore
	caps        map[peer.ID]*domain.PeerCapabilities
	banned      map[peer.ID]bool
	failCount   map[peer.ID]int
	findResults map[domain.TrackID][]peer.ID
}

func newMockPeerManager() *mockPeerManager {
	return &mockPeerManager{
		peers:       make(map[peer.ID]*domain.PeerInfo),
		scores:      make(map[peer.ID]*domain.PeerScore),
		caps:        make(map[peer.ID]*domain.PeerCapabilities),
		banned:      make(map[peer.ID]bool),
		failCount:   make(map[peer.ID]int),
		findResults: make(map[domain.TrackID][]peer.ID),
	}
}

func (m *mockPeerManager) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []peer.ID {
	return m.findResults[trackID]
}

func (m *mockPeerManager) GetPeerScore(ctx context.Context, pid peer.ID) *domain.PeerScore {
	return m.scores[pid]
}

func (m *mockPeerManager) RecordSuccess(pid peer.ID) {
	delete(m.failCount, pid)
}

func (m *mockPeerManager) RecordFailure(pid peer.ID) {
	m.failCount[pid]++
}

func (m *mockPeerManager) IsBanned(pid peer.ID) bool {
	return m.banned[pid]
}

func (m *mockPeerManager) GetPeerCapabilities(pid peer.ID) *domain.PeerCapabilities {
	return m.caps[pid]
}

func (m *mockPeerManager) GetTrackOwners(trackID string) []peer.ID {
	for tid, owners := range m.findResults {
		if string(tid) == trackID {
			return owners
		}
	}
	return nil
}

func (m *mockPeerManager) GetPeerLatency(pid peer.ID) time.Duration {
	return 0
}

type mockLibraryRepo struct {
	tracks map[domain.TrackID]*domain.Track
}

func newMockLibraryRepo() *mockLibraryRepo {
	return &mockLibraryRepo{
		tracks: make(map[domain.TrackID]*domain.Track),
	}
}

func (m *mockLibraryRepo) Save(ctx context.Context, track *domain.Track) error {
	m.tracks[track.ID] = track
	return nil
}

func (m *mockLibraryRepo) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	if track, ok := m.tracks[id]; ok {
		return track, nil
	}
	return nil, domain.ErrTrackNotFound
}

func (m *mockLibraryRepo) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	return nil, nil
}

func (m *mockLibraryRepo) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	return nil, nil
}

func (m *mockLibraryRepo) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	return nil, nil
}

func (m *mockLibraryRepo) Delete(ctx context.Context, id domain.TrackID) error {
	delete(m.tracks, id)
	return nil
}

func (m *mockLibraryRepo) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	return nil
}

func (m *mockLibraryRepo) ListAll(ctx context.Context) ([]*domain.Track, error) {
	return nil, nil
}

func (m *mockLibraryRepo) ListAllPaths(ctx context.Context) ([]string, error) {
	return nil, nil
}

func TestP2PResolver_ResolveLocal(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/test.mp3")
	tmpFile := t.TempDir() + "/test.mp3"
	if err := os.WriteFile(tmpFile, []byte("test data"), 0o644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	repo.tracks[trackID] = &domain.Track{
		ID:   trackID,
		Path: tmpFile,
	}

	reader, err := resolver.Resolve(context.Background(), trackID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if reader == nil {
		t.Fatal("Resolve() returned nil reader")
	}
	reader.Close()
}

func TestP2PResolver_ResolveRemote(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/remote.mp3")
	pid := peer.ID("remote-peer")
	peerMgr.findResults[trackID] = []peer.ID{pid}
	peerMgr.scores[pid] = &domain.PeerScore{
		PeerID:       pid,
		SuccessCount: 10,
		FailureCount: 0,
		AvgLatency:   100 * time.Millisecond,
		AvgBandwidth: 1000000,
	}
	peerMgr.caps[pid] = &domain.PeerCapabilities{
		SupportedCodecs: []string{"mp3", "flac"},
	}

	reader, err := resolver.Resolve(context.Background(), trackID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if reader == nil {
		t.Fatal("Resolve() returned nil reader")
	}
	reader.Close()
}

func TestP2PResolver_TrackNotFound(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/nonexistent.mp3")
	_, err := resolver.Resolve(context.Background(), trackID)
	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Resolve() error = %v, want ErrTrackNotFound", err)
	}
}

func TestPeerScorer_RecordSuccess(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")

	scorer.RecordFailure(pid)
	scorer.RecordFailure(pid)
	scorer.RecordFailure(pid)

	if !scorer.IsBanned(pid) {
		t.Error("IsBanned() = false, want true after 3 failures")
	}

	scorer.RecordSuccess(pid)

	if scorer.IsBanned(pid) {
		t.Error("IsBanned() = true, want false after success")
	}
}

func TestPeerScorer_RecordFailure(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")

	scorer.RecordFailure(pid)
	scorer.RecordFailure(pid)

	count := scorer.failures[pid]
	if count != 2 {
		t.Errorf("failures[%s] = %d, want 2", pid, count)
	}
}

func TestPeerScorer_Cooldown(t *testing.T) {
	scorer := NewPeerScorer()
	scorer.failLimit = 2
	scorer.cooldown = 100 * time.Millisecond

	pid := peer.ID("test-peer")
	scorer.RecordFailure(pid)
	scorer.RecordFailure(pid)

	if !scorer.IsBanned(pid) {
		t.Error("IsBanned() = false, want true after 2 failures")
	}

	time.Sleep(150 * time.Millisecond)

	if scorer.IsBanned(pid) {
		t.Error("IsBanned() = true, want false after cooldown")
	}
}

func TestPeerScorer_Score(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")

	score := scorer.Score(pid, 100*time.Millisecond, 1000000, 0.9)
	if score <= 0 {
		t.Errorf("Score() = %f, want > 0", score)
	}
}
