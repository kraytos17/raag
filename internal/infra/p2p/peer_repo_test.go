package p2p

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	protocols "github.com/p-society/raag/internal/infra/p2p/protocols"
)

func TestP2PNode_RefreshAllManifests(t *testing.T) {
	net := mocknet.New()
	defer net.Close()

	hostA, err := net.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer A: %v", err)
	}
	hostB, err := net.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer B: %v", err)
	}
	if err := net.LinkAll(); err != nil {
		t.Fatalf("LinkAll: %v", err)
	}
	if err := hostA.Connect(context.Background(), hostB.Peerstore().PeerInfo(hostB.ID())); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Peer B serves a manifest with one track.
	track := &domain.Track{ID: domain.TrackID("refresh-1"), Path: "/tmp/refresh.mp3"}
	repo := &manifestsTestLibrary{track: track}
	sh := protocols.NewSyncHandler(repo, nil, hostB.ID())
	sh.SetAnnounceLibrary(true)
	hostB.SetStreamHandler(protocols.SyncProtocol, sh.Handle)

	// Node A's peer manager, wired to its host, fetches from B.
	pm := newPeerManager(hostA, discovery.NewPeerCache(100), nil, nil, time.Hour, nil)
	node := &P2PNode{host: hostA, peerMgr: pm}

	node.refreshAllManifests()

	// The manifest must now be cached for peer B.
	deadline := time.Now().Add(5 * time.Second)
	for {
		pm.mu.RLock()
		manifest, ok := pm.manifests[hostB.ID()]
		pm.mu.RUnlock()
		if ok && manifest != nil && slices.Contains(manifest.data.TrackIds, string(track.ID)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for peer B's manifest to be cached")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestP2PNode_OverlayLiveScore_MergesScorerSnapshot verifies PeerStatus
// overlays the live scorer latency/bandwidth/score onto the persisted peer.
func TestP2PNode_OverlayLiveScore_MergesScorerSnapshot(t *testing.T) {
	net := mocknet.New()
	defer net.Close()
	host, err := net.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer: %v", err)
	}

	scorer := NewPeerScorer()
	node := &P2PNode{host: host, scorer: scorer}

	pid := peer.ID("live-peer")
	info := domain.NewPeerInfo(pid, []string{"/ip4/10.0.0.1/tcp/7844"})
	scorer.RecordLatency(pid, 12*time.Millisecond)
	scorer.RecordBandwidth(pid, 1_200_000)
	scorer.RecordSuccess(pid)
	scorer.RecordSuccess(pid)

	pbPeer := node.PeerStatus(info)
	if pbPeer == nil {
		t.Fatal("PeerStatus() = nil")
	}
	if pbPeer.Score == nil {
		t.Fatal("expected live score to be attached")
	}
	if pbPeer.Score.AvgLatencyMs != 12 {
		t.Errorf("AvgLatencyMs = %v, want 12", pbPeer.Score.AvgLatencyMs)
	}
	if pbPeer.Score.AvgBandwidth != 1_200_000 {
		t.Errorf("AvgBandwidth = %d, want 1200000", pbPeer.Score.AvgBandwidth)
	}
	if pbPeer.Score.SuccessCount != 2 {
		t.Errorf("SuccessCount = %d, want 2", pbPeer.Score.SuccessCount)
	}
	// score = latency*0.5 + successRate*0.4 + bandwidth*0.1, all > 0 here.
	if pbPeer.Score.Score <= 0 {
		t.Errorf("Score = %v, want > 0", pbPeer.Score.Score)
	}
}

// TestP2PNode_MeasureAllPeersLatency_ActivePingRecords verifies the latency
// pass actively pings connected peers and records the RTT to the scorer.
func TestP2PNode_MeasureAllPeersLatency_ActivePingRecords(t *testing.T) {
	net := mocknet.New()
	defer net.Close()

	hostA, err := net.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer A: %v", err)
	}

	hostB, err := net.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer B: %v", err)
	}

	pingA := ping.NewPingService(hostA)
	pingB := ping.NewPingService(hostB)
	_ = pingA
	_ = pingB

	if err := net.LinkAll(); err != nil {
		t.Fatalf("LinkAll: %v", err)
	}
	if err := hostA.Connect(context.Background(), hostB.Peerstore().PeerInfo(hostB.ID())); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	scorer := NewPeerScorer()
	pm := newPeerManager(hostA, discovery.NewPeerCache(100), nil, scorer, time.Hour, nil)
	pm.peerCache.Add(hostB.Peerstore().PeerInfo(hostB.ID()))
	node := &P2PNode{
		host:      hostA,
		peerMgr:   pm,
		peerCache: pm.peerCache,
		scorer:    scorer,
	}

	node.measureAllPeersLatency()
	if got := scorer.AvgLatency(hostB.ID()); got <= 0 {
		t.Fatalf("AvgLatency(peer) = %v, want > 0 after active probe", got)
	}
}

// manifestsTestLibrary returns a single track from FindByID and ListAll.
type manifestsTestLibrary struct {
	track *domain.Track
}

func (l *manifestsTestLibrary) Save(ctx context.Context, track *domain.Track) error { return nil }
func (l *manifestsTestLibrary) FindByID(ctx context.Context, id domain.TrackID) (*domain.Track, error) {
	if l.track != nil && l.track.ID == id {
		return l.track, nil
	}
	return nil, domain.ErrTrackNotFound
}

func (l *manifestsTestLibrary) FindByIDs(ctx context.Context, ids []domain.TrackID) ([]*domain.Track, error) {
	return nil, nil
}

func (l *manifestsTestLibrary) FindByPath(ctx context.Context, path string) (*domain.Track, error) {
	return nil, domain.ErrTrackNotFound
}

func (l *manifestsTestLibrary) GetCoverArt(ctx context.Context, id domain.TrackID) ([]byte, error) {
	return nil, nil
}
func (l *manifestsTestLibrary) Delete(ctx context.Context, id domain.TrackID) error { return nil }
func (l *manifestsTestLibrary) BulkSave(ctx context.Context, tracks []*domain.Track) error {
	return nil
}

func (l *manifestsTestLibrary) ListAll(ctx context.Context) ([]*domain.Track, error) {
	if l.track != nil {
		return []*domain.Track{l.track}, nil
	}
	return nil, nil
}

func (l *manifestsTestLibrary) ListAllPaths(ctx context.Context) ([]string, error) { return nil, nil }

// TestP2PNode_DiscoveredPeers verifies AddDiscovered/DiscoveredPeers surface
// discovered-but-unconnected peers
func TestP2PNode_DiscoveredPeers(t *testing.T) {
	net := mocknet.New()
	defer net.Close()

	hostA, err := net.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer A: %v", err)
	}

	node := &P2PNode{
		host:           hostA,
		mdnsDiscovered: discovery.NewTTLPeerCache(100, time.Hour),
	}
	defer node.mdnsDiscovered.Close()

	if got := len(node.DiscoveredPeers()); got != 0 {
		t.Fatalf("DiscoveredPeers() len = %d, want 0 initially", got)
	}

	pi := peer.AddrInfo{ID: peer.ID("discovered-peer")}
	node.AddDiscovered(pi)

	if got := len(node.DiscoveredPeers()); got != 1 {
		t.Fatalf("DiscoveredPeers() len = %d, want 1 after AddDiscovered", got)
	}
	if node.DiscoveredPeers()[0].ID != pi.ID {
		t.Errorf("DiscoveredPeers()[0].ID = %v, want %v", node.DiscoveredPeers()[0].ID, pi.ID)
	}
}
