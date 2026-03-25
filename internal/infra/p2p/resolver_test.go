package p2p

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
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
	if c, ok := m.caps[pid]; ok {
		return c
	}
	return &domain.PeerCapabilities{}
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
	defer pool.Close()
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
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(data) != "test data" {
		t.Fatalf("ReadAll() = %q, want %q", data, "test data")
	}
	reader.Close()
}

func TestP2PResolver_ResolveLocal_FileNotFound(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/missing.mp3")
	repo.tracks[trackID] = &domain.Track{
		ID:   trackID,
		Path: "/nonexistent/path/missing.mp3",
	}

	_, err := resolver.Resolve(context.Background(), trackID)
	if err == nil {
		t.Fatal("Resolve() should fail for missing file")
	}
}

func TestP2PResolver_ResolveRemote(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
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
	defer pool.Close()
	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/nonexistent.mp3")
	_, err := resolver.Resolve(context.Background(), trackID)
	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Resolve() error = %v, want ErrTrackNotFound", err)
	}
}

func TestP2PResolver_ResolveRemote_AllPeersBanned(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/banned.mp3")
	pid := peer.ID("banned-peer")
	peerMgr.findResults[trackID] = []peer.ID{pid}
	peerMgr.banned[pid] = true
	peerMgr.caps[pid] = &domain.PeerCapabilities{}

	_, err := resolver.Resolve(context.Background(), trackID)
	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Resolve() error = %v, want ErrTrackNotFound when all peers banned", err)
	}
}

func TestP2PResolver_ResolveRemote_NoPeersHaveTrack(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	peerMgr := newMockPeerManager()
	scorer := NewPeerScorer()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	trackID := domain.GenerateTrackID("/music/orphan.mp3")
	peerMgr.findResults[trackID] = []peer.ID{} // empty peers

	_, err := resolver.Resolve(context.Background(), trackID)
	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("Resolve() error = %v, want ErrTrackNotFound for empty peers", err)
	}
}

func TestP2PResolver_LastPeerID_InitiallyEmpty(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	if pid := resolver.LastPeerID(); pid != "" {
		t.Fatalf("initial LastPeerID() = %q, want empty", pid)
	}
}

func TestP2PResolver_LastPeerID_ConcurrentAccess(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)

	// Simulate concurrent writes and reads to lastPeerID.
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			// Write via the lock (simulating what tryPeers does)
			resolver.lastPeerMu.Lock()
			resolver.lastPeerID = peer.ID("peer-" + string(rune('A'+n%26)))
			resolver.lastPeerMu.Unlock()
		}(i)
		go func() {
			defer wg.Done()
			_ = resolver.LastPeerID()
			_ = resolver.LastPeerIDString()
		}()
	}
	wg.Wait()
}

func TestP2PResolver_FindPeersWithTrack(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	trackID := domain.TrackID("track-abc")
	p1 := peer.ID("peer-1")
	p2 := peer.ID("peer-2")
	peerMgr.findResults[trackID] = []peer.ID{p1, p2}

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	result := resolver.FindPeersWithTrack(context.Background(), trackID)

	if len(result) != 2 {
		t.Fatalf("FindPeersWithTrack() len = %d, want 2", len(result))
	}
	if result[0] != p1.String() || result[1] != p2.String() {
		t.Fatalf("FindPeersWithTrack() = %v, want [%s, %s]", result, p1, p2)
	}
}

func TestP2PResolver_FindPeersWithTrack_NoneFound(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	result := resolver.FindPeersWithTrack(context.Background(), "nonexistent")
	if len(result) != 0 {
		t.Fatalf("FindPeersWithTrack() len = %d, want 0", len(result))
	}
}

func TestP2PResolver_GetPeerLatency(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()
	pid := peer.ID("peer-1")

	scorer.RecordLatency(pid, 100*time.Millisecond)
	scorer.RecordLatency(pid, 200*time.Millisecond)

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	lat := resolver.GetPeerLatency(pid)
	if lat != 150*time.Millisecond {
		t.Fatalf("GetPeerLatency() = %v, want 150ms", lat)
	}
}

func TestP2PResolver_GetPeerLatency_NilScorer(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	peerMgr := newMockPeerManager()

	resolver := NewP2PResolver(repo, pool, peerMgr, nil, host)
	lat := resolver.GetPeerLatency(peer.ID("peer-1"))
	if lat != 0 {
		t.Fatalf("GetPeerLatency() = %v, want 0 with nil scorer", lat)
	}
}

func TestStreamingReader_DoubleClose(t *testing.T) {
	inner := io.NopCloser(bytes.NewReader([]byte("hello")))
	sr := &streamingReader{
		reader:  inner,
		trackID: "track-1",
		peerID:  peer.ID("peer-1"),
	}

	if err := sr.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := sr.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestStreamingReader_ReadThenClose(t *testing.T) {
	data := []byte("hello world")
	inner := io.NopCloser(bytes.NewReader(data))
	sr := &streamingReader{
		reader:  inner,
		trackID: "track-1",
		peerID:  peer.ID("peer-1"),
	}

	buf := make([]byte, 5)
	n, err := sr.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 || string(buf) != "hello" {
		t.Fatalf("Read() = %d, %q; want 5, %q", n, buf, "hello")
	}

	if err := sr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
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

	scorer.mu.RLock()
	count := scorer.failures[pid]
	scorer.mu.RUnlock()
	if count != 2 {
		t.Errorf("failures[%s] = %d, want 2", pid, count)
	}
}

func TestPeerScorer_Cooldown(t *testing.T) {
	scorer := NewPeerScorerWithConfig(2, 100*time.Millisecond)

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

func TestPeerScorer_ScoreComponents(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	// Zero latency, zero bandwidth, zero success
	s1 := scorer.Score(pid, 0, 0, 0)
	// High latency should reduce score
	s2 := scorer.Score(pid, 10*time.Second, 0, 0)
	if s2 >= s1 {
		t.Errorf("higher latency should reduce score: s1=%f, s2=%f", s1, s2)
	}

	// Higher bandwidth should increase score
	s3 := scorer.Score(pid, 0, 10_000_000, 0.9)
	s4 := scorer.Score(pid, 0, 100_000_000, 0.9)
	if s4 <= s3 {
		t.Errorf("higher bandwidth should increase score: s3=%f, s4=%f", s3, s4)
	}
}

func TestPeerScorer_RecordLatency(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("test-peer")

	if lat := scorer.AvgLatency(pid); lat != 0 {
		t.Fatalf("AvgLatency() = %v, want 0 for unknown peer", lat)
	}

	scorer.RecordLatency(pid, 100*time.Millisecond)
	scorer.RecordLatency(pid, 200*time.Millisecond)

	avg := scorer.AvgLatency(pid)
	if avg != 150*time.Millisecond {
		t.Fatalf("AvgLatency() = %v, want 150ms", avg)
	}
}

func TestPeerScorer_RecordLatency_SlidingWindow(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	// Record 7 latencies, window is 5
	for i := range 7 {
		scorer.RecordLatency(pid, time.Duration(i+1)*time.Millisecond)
	}

	scorer.mu.RLock()
	count := len(scorer.latencies[pid])
	scorer.mu.RUnlock()
	if count != 5 {
		t.Fatalf("latency window = %d, want 5", count)
	}

	// avg of 3,4,5,6,7 = 5
	avg := scorer.AvgLatency(pid)
	if avg != 5*time.Millisecond {
		t.Fatalf("AvgLatency() = %v, want 5ms", avg)
	}
}

func TestPeerScorer_ClearLatency(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	scorer.RecordLatency(pid, 100*time.Millisecond)
	scorer.ClearLatency(pid)

	if lat := scorer.AvgLatency(pid); lat != 0 {
		t.Fatalf("AvgLatency() = %v after clear, want 0", lat)
	}
}

func TestPeerScorer_RecordBandwidth(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	if bw := scorer.AvgBandwidth(pid); bw != 0 {
		t.Fatalf("AvgBandwidth() = %d, want 0 for unknown peer", bw)
	}

	scorer.RecordBandwidth(pid, 1000)
	scorer.RecordBandwidth(pid, 2000)

	if bw := scorer.AvgBandwidth(pid); bw != 1500 {
		t.Fatalf("AvgBandwidth() = %d, want 1500", bw)
	}
}

func TestPeerScorer_RecordBandwidth_IgnoresNonPositive(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	scorer.RecordBandwidth(pid, 0)
	scorer.RecordBandwidth(pid, -100)

	if bw := scorer.AvgBandwidth(pid); bw != 0 {
		t.Fatalf("AvgBandwidth() = %d, want 0 after non-positive recordings", bw)
	}
}

func TestPeerScorer_RecordBandwidth_SlidingWindow(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	for i := range 7 {
		scorer.RecordBandwidth(pid, int64((i+1)*1000))
	}

	scorer.mu.RLock()
	count := len(scorer.bandwidths[pid])
	scorer.mu.RUnlock()
	if count != 5 {
		t.Fatalf("bandwidth window = %d, want 5", count)
	}
}

func TestPeerScorer_Snapshot(t *testing.T) {
	scorer := NewPeerScorerWithConfig(5, time.Minute)
	pid := peer.ID("p")

	scorer.RecordLatency(pid, 100*time.Millisecond)
	scorer.RecordBandwidth(pid, 5000)
	scorer.RecordSuccess(pid)
	scorer.RecordSuccess(pid)
	scorer.RecordFailure(pid)

	avgLat, avgBw, successes, failures, banned := scorer.Snapshot(pid)

	if avgLat != 100*time.Millisecond {
		t.Errorf("Snapshot avgLat = %v, want 100ms", avgLat)
	}
	if avgBw != 5000 {
		t.Errorf("Snapshot avgBw = %d, want 5000", avgBw)
	}
	if successes != 2 {
		t.Errorf("Snapshot successes = %d, want 2", successes)
	}
	if failures != 1 {
		t.Errorf("Snapshot failures = %d, want 1", failures)
	}
	if banned {
		t.Error("Snapshot banned = true, want false (under threshold)")
	}
}

func TestPeerScorer_Snapshot_BannedExpires(t *testing.T) {
	scorer := NewPeerScorerWithConfig(1, 50*time.Millisecond)
	pid := peer.ID("p")

	scorer.RecordFailure(pid)
	snap := func() bool {
		_, _, _, _, b := scorer.Snapshot(pid)
		return b
	}
	if !snap() {
		t.Error("should be banned immediately after failure")
	}

	time.Sleep(100 * time.Millisecond)
	banned := snap()
	if banned {
		t.Error("should not be banned after cooldown")
	}
}

func TestPeerScorer_Reset(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	scorer.RecordLatency(pid, 100*time.Millisecond)
	scorer.RecordBandwidth(pid, 5000)
	scorer.RecordSuccess(pid)
	scorer.RecordFailure(pid)

	scorer.Reset(pid)

	if lat := scorer.AvgLatency(pid); lat != 0 {
		t.Errorf("after Reset, AvgLatency = %v, want 0", lat)
	}
	if bw := scorer.AvgBandwidth(pid); bw != 0 {
		t.Errorf("after Reset, AvgBandwidth = %d, want 0", bw)
	}
	if scorer.IsBanned(pid) {
		t.Error("after Reset, IsBanned = true, want false")
	}
}

func TestPeerScorer_ConcurrentAccess(t *testing.T) {
	scorer := NewPeerScorer()
	pid := peer.ID("p")

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			switch n % 6 {
			case 0:
				scorer.RecordLatency(pid, time.Duration(n)*time.Millisecond)
			case 1:
				scorer.RecordBandwidth(pid, int64(n*1000))
			case 2:
				scorer.RecordSuccess(pid)
			case 3:
				scorer.RecordFailure(pid)
			case 4:
				scorer.IsBanned(pid)
			case 5:
				scorer.Score(pid, time.Millisecond, 1000, 0.5)
			}
		}(i)
	}
	wg.Wait()
}

func TestNewPeerScorerWithConfig(t *testing.T) {
	scorer := NewPeerScorerWithConfig(10, 2*time.Minute)
	pid := peer.ID("p")

	// Need 10 failures to ban
	for range 9 {
		scorer.RecordFailure(pid)
	}
	if scorer.IsBanned(pid) {
		t.Error("should not be banned after 9 failures with limit 10")
	}
	scorer.RecordFailure(pid)
	if !scorer.IsBanned(pid) {
		t.Error("should be banned after 10 failures with limit 10")
	}
}

func TestP2PResolverAdapter_LastPeerID(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	adapter := NewP2PResolverAdapter(resolver)

	// Set via resolver and verify adapter reads it through the same lock
	testPeer := peer.ID("peer-42")
	resolver.lastPeerMu.Lock()
	resolver.lastPeerID = testPeer
	resolver.lastPeerMu.Unlock()

	if got := adapter.LastPeerID(); got != testPeer.String() {
		t.Fatalf("adapter.LastPeerID() = %q, want %q", got, testPeer.String())
	}
}

func TestP2PResolverAdapter_FindPeersWithTrack(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	trackID := domain.TrackID("track-1")
	peerMgr.findResults[trackID] = []peer.ID{"p1", "p2"}

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	adapter := NewP2PResolverAdapter(resolver)

	result := adapter.FindPeersWithTrack(context.Background(), trackID)
	if len(result) != 2 {
		t.Fatalf("FindPeersWithTrack() len = %d, want 2", len(result))
	}
}

func TestP2PResolverAdapter_Resolve_Local(t *testing.T) {
	repo := newMockLibraryRepo()
	host := newMockHost()
	pool := NewStreamPool(host, "/raag/stream/1.0.0")
	defer pool.Close()
	scorer := NewPeerScorer()
	peerMgr := newMockPeerManager()

	trackID := domain.GenerateTrackID("/music/test.mp3")
	tmpFile := t.TempDir() + "/test.mp3"
	if err := os.WriteFile(tmpFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo.tracks[trackID] = &domain.Track{ID: trackID, Path: tmpFile}

	resolver := NewP2PResolver(repo, pool, peerMgr, scorer, host)
	adapter := NewP2PResolverAdapter(resolver)

	reader, err := adapter.Resolve(context.Background(), trackID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	reader.Close()
}
