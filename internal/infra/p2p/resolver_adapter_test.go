package p2p

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
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

func TestP2PResolverAdapter_SearchPeerPreservesErrorCode(t *testing.T) {
	var frame bytes.Buffer
	if err := wire.WriteMsg(&frame, &pb.SyncResponse{Payload: &pb.SyncResponse_Error{
		Error: &pb.ErrorResponse{Message: "slow down", Code: pb.ErrorCode_ERROR_CODE_RATE_LIMITED},
	}}); err != nil {
		t.Fatalf("encode error response: %v", err)
	}
	seqHost := &resolverSequenceHost{streams: []network.Stream{&resolverFrameStream{
		mockStream: &mockStream{id: "search-error"},
		response:   frame.Bytes(),
	}}}
	resolver := NewP2PResolver(nil, nil, nil, nil, seqHost)
	adapter := NewP2PResolverAdapter(resolver)

	_, err := adapter.SearchPeer(context.Background(), peer.ID("search-peer"), "query", 10)
	var syncErr *SyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("SearchPeer() error = %v, want *SyncError", err)
	}
	if syncErr.Code != pb.ErrorCode_ERROR_CODE_RATE_LIMITED {
		t.Errorf("SyncError.Code = %v, want RATE_LIMITED", syncErr.Code)
	}
}

func TestP2PResolverAdapter_FetchTrackMetadataPreservesErrorCode(t *testing.T) {
	var frame bytes.Buffer
	if err := wire.WriteMsg(&frame, &pb.SyncResponse{Payload: &pb.SyncResponse_Error{
		Error: &pb.ErrorResponse{Message: "track not found", Code: pb.ErrorCode_ERROR_CODE_TRACK_NOT_FOUND},
	}}); err != nil {
		t.Fatalf("encode error response: %v", err)
	}
	seqHost := &resolverSequenceHost{streams: []network.Stream{&resolverFrameStream{
		mockStream: &mockStream{id: "track-detail-error"},
		response:   frame.Bytes(),
	}}}
	resolver := NewP2PResolver(nil, nil, nil, nil, seqHost)
	adapter := NewP2PResolverAdapter(resolver)

	_, pub, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateEd25519Key: %v", err)
	}
	detailPeer, err := peer.IDFromPublicKey(pub)
	if err != nil {
		t.Fatalf("IDFromPublicKey: %v", err)
	}
	_, err = adapter.FetchTrackMetadata(context.Background(), domain.TrackID(track1ID), detailPeer.String())
	var syncErr *SyncError
	if !errors.As(err, &syncErr) {
		t.Fatalf("FetchTrackMetadata() error = %v, want *SyncError", err)
	}
	if syncErr.Code != pb.ErrorCode_ERROR_CODE_TRACK_NOT_FOUND {
		t.Errorf("SyncError.Code = %v, want TRACK_NOT_FOUND", syncErr.Code)
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
