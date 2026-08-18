package discovery

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
)

// connectOverrideHost wraps a host.Host and lets tests override Connect.
type connectOverrideHost struct {
	host.Host
	connect func(ctx context.Context, pi peer.AddrInfo) error
}

func (h *connectOverrideHost) Connect(ctx context.Context, pi peer.AddrInfo) error {
	return h.connect(ctx, pi)
}

// newNotifee builds a Notifee over an in-memory host with a single connect slot.
func newNotifee(t *testing.T, h host.Host) *Notifee {
	t.Helper()
	return &Notifee{
		host:    h,
		done:    make(chan struct{}),
		sem:     make(chan struct{}, 1),
		pending: make(chan peer.AddrInfo, pendingQueueSize),
	}
}

func TestNotifee_QueuesWhenSemFull(t *testing.T) {
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

	connectedCh := make(chan struct{}, 1)
	n := newNotifee(t, hostA)
	n.onConnected = func(pi peer.AddrInfo) {
		connectedCh <- struct{}{}
	}

	// Occupy the single connect slot before any peer is handled.
	n.sem <- struct{}{}
	// HandlePeerFound must queue the peer rather than drop it.
	n.HandlePeerFound(hostB.Peerstore().PeerInfo(hostB.ID()))
	if len(n.pending) != 1 {
		t.Fatalf("pending queue len = %d, want 1 (peer queued when semaphore full)", len(n.pending))
	}

	// Start workers and release the slot: the queued peer should connect.
	n.startWorkers()
	<-n.sem
	select {
	case <-connectedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("queued peer never connected after slot released")
	}
}

func TestNotifee_RetriesOnConnectFailure(t *testing.T) {
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

	var attempts atomic.Int32
	wrapped := &connectOverrideHost{
		Host: hostA,
		connect: func(ctx context.Context, pi peer.AddrInfo) error {
			if attempts.Add(1) <= 2 {
				return errors.New("simulated connect failure")
			}
			return hostA.Connect(ctx, pi)
		},
	}

	connectedCh := make(chan struct{}, 1)
	n := newNotifee(t, wrapped)
	n.onConnected = func(pi peer.AddrInfo) {
		connectedCh <- struct{}{}
	}

	n.startWorkers()
	n.HandlePeerFound(hostB.Peerstore().PeerInfo(hostB.ID()))
	select {
	case <-connectedCh:
		if got := attempts.Load(); got < 3 {
			t.Errorf("Connect called %d times, want >= 3 (retries on failure)", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("peer never connected after retries")
	}
}

func TestNotifee_SkipsSelfAndConnected(t *testing.T) {
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

	n := newNotifee(t, hostA)
	n.startWorkers()
	// Self must be skipped.
	n.HandlePeerFound(hostA.Peerstore().PeerInfo(hostA.ID()))
	// Already-connected peer must be skipped.
	n.HandlePeerFound(hostB.Peerstore().PeerInfo(hostB.ID()))

	select {
	case <-n.pending:
		t.Fatal("self or connected peer was queued")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestPeerCache_NoTTL(t *testing.T) {
	c := NewPeerCache(10)
	pid := peer.ID("peer-1")
	c.Add(peer.AddrInfo{ID: pid})
	if len(c.All()) != 1 {
		t.Fatalf("All() len = %d, want 1", len(c.All()))
	}

	c.Remove(pid)
	if len(c.All()) != 0 {
		t.Fatalf("All() len after Remove = %d, want 0", len(c.All()))
	}
}

func TestTTLPeerCache_Expires(t *testing.T) {
	c := NewTTLPeerCache(10, 100*time.Millisecond)
	defer c.Close()
	pid := peer.ID("peer-1")
	c.Add(peer.AddrInfo{ID: pid})
	if len(c.All()) != 1 {
		t.Fatalf("All() before TTL len = %d, want 1", len(c.All()))
	}

	deadline := time.Now().Add(2 * time.Second)
	for len(c.All()) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("TTL peer cache did not expire entries")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

var _ host.Host = (*connectOverrideHost)(nil)
