package p2p

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/domain"
)

func TestP2PResolverAdapter_PeersConcurrentProviderUpdates(t *testing.T) {
	resolver := &P2PResolverAdapter{}
	providers := []func() []peer.ID{
		func() []peer.ID { return []peer.ID{"peer-a"} },
		func() []peer.ID { return []peer.ID{"peer-b", "peer-c"} },
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				for _, provider := range providers {
					resolver.SetPeersProvider(provider)
					_ = resolver.Peers()
				}
			}
		})
	}
	wg.Wait()
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

	trackID := domain.TrackID(track1ID)
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
