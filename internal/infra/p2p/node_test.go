package p2p

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/p-society/raag/internal/infra/events"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	"github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/testutil/mocks"
)

type lifecycleTestDiscovery struct {
	startErr error
	starts   int
	closes   int
	done     <-chan struct{}
	mu       sync.Mutex
}

func (d *lifecycleTestDiscovery) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.starts++
	return d.startErr
}

func (d *lifecycleTestDiscovery) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closes++
	return nil
}

func newLifecycleTestNode(t *testing.T, broadcastEnabled bool) *P2PNode {
	t.Helper()
	privateKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateEd25519Key: %v", err)
	}
	gater := NewPeerGater()
	gater.SetLANOnly(true)
	libp2pHost, err := NewHost(privateKey, P2PConfig{
		ListenAddrs:     []string{"/ip4/127.0.0.1/tcp/0"},
		LANOnly:         true,
		ConnectionGater: gater,
	})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	t.Cleanup(func() { _ = libp2pHost.Close() })

	libraryRepo := mocks.NewMockLibraryRepository()
	pool := NewStreamPool(libp2pHost, protocols.StreamProtocol)
	peerCache := discovery.NewPeerCache(10)
	discovered := discovery.NewTTLPeerCache(10, discovery.MdnsDiscoveryTTL)
	scorer := NewPeerScorer()
	peerMgr := newPeerManager(libp2pHost, peerCache, libraryRepo, scorer, time.Hour, nil)
	syncHandler := protocols.NewSyncHandler(libraryRepo, nil, libp2pHost.ID())
	streamHandler := protocols.NewStreamHandler(libraryRepo, nil)
	admission := protocols.NewAdmissionRegistry()
	syncHandler.SetAdmissionRegistry(admission)
	streamHandler.SetAdmissionRegistry(admission)
	resolver := NewP2PResolver(libraryRepo, pool, peerMgr, scorer, libp2pHost)
	node := &P2PNode{
		host:             libp2pHost,
		gater:            gater,
		streamPool:       pool,
		streamHandler:    streamHandler,
		syncHandler:      syncHandler,
		resolver:         resolver,
		scorer:           scorer,
		peerCache:        peerCache,
		mdnsDiscovered:   discovered,
		peerMgr:          peerMgr,
		mdnsServiceName:  "raag-test",
		admission:        admission,
		lanOnly:          true,
		maxPeers:         10,
		peerDataTTL:      time.Hour,
		broadcastEnabled: broadcastEnabled,
	}
	t.Cleanup(func() { _ = node.Stop(context.Background()) })
	return node
}

var _ peerDiscoveryService = (*lifecycleTestDiscovery)(nil)

func TestP2PNode_StartFailureStopsNodeAndRejectsRetry(t *testing.T) {
	node := newLifecycleTestNode(t, false)
	startErr := errors.New("simulated mDNS start failure")
	mdnsService := &lifecycleTestDiscovery{startErr: startErr}
	node.mdnsFactory = func(done <-chan struct{}, _ discovery.PeerHandler, _ discovery.PeerHandler) peerDiscoveryService {
		mdnsService.done = done
		return mdnsService
	}

	if err := node.Start(context.Background(), nil); !errors.Is(err, startErr) {
		t.Fatalf("Start error = %v, want %v", err, startErr)
	}
	if mdnsService.closes != 1 {
		t.Fatalf("mDNS Close calls = %d, want 1", mdnsService.closes)
	}
	if mdnsService.done == nil {
		t.Fatal("mDNS lifecycle context was not provided")
	}
	select {
	case <-mdnsService.done:
	default:
		t.Fatal("mDNS lifecycle context was not canceled after startup failure")
	}
	if err := node.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after failed Start: %v", err)
	}
	if err := node.Start(context.Background(), nil); !errors.Is(err, ErrP2PNodeStopped) {
		t.Fatalf("Start after failed startup error = %v, want ErrP2PNodeStopped", err)
	}
}

func TestP2PNode_BroadcastStartFailureClosesStartedDiscovery(t *testing.T) {
	node := newLifecycleTestNode(t, true)
	mdnsService := &lifecycleTestDiscovery{}
	broadcastErr := errors.New("simulated broadcast start failure")
	broadcastService := &lifecycleTestDiscovery{startErr: broadcastErr}
	node.mdnsFactory = func(done <-chan struct{}, _ discovery.PeerHandler, _ discovery.PeerHandler) peerDiscoveryService {
		mdnsService.done = done
		return mdnsService
	}
	node.broadcastFactory = func(<-chan struct{}, host.Host, int, string, discovery.PeerHandler, discovery.PeerHandler) peerDiscoveryService {
		return broadcastService
	}

	if err := node.Start(context.Background(), nil); !errors.Is(err, broadcastErr) {
		t.Fatalf("Start error = %v, want %v", err, broadcastErr)
	}
	if mdnsService.starts != 1 || mdnsService.closes != 1 {
		t.Fatalf("mDNS starts/closes = %d/%d, want 1/1", mdnsService.starts, mdnsService.closes)
	}
	if broadcastService.starts != 1 || broadcastService.closes != 1 {
		t.Fatalf("broadcast starts/closes = %d/%d, want 1/1", broadcastService.starts, broadcastService.closes)
	}
}

func TestP2PNode_StopIsIdempotentAndPreventsRestart(t *testing.T) {
	node := newLifecycleTestNode(t, false)
	mdnsService := &lifecycleTestDiscovery{}
	node.mdnsFactory = func(done <-chan struct{}, _ discovery.PeerHandler, _ discovery.PeerHandler) peerDiscoveryService {
		mdnsService.done = done
		return mdnsService
	}
	bus := events.New()

	if err := node.Start(context.Background(), bus); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := node.Start(context.Background(), bus); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if mdnsService.starts != 1 {
		t.Fatalf("mDNS Start calls = %d, want 1", mdnsService.starts)
	}

	if err := node.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := node.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
	if mdnsService.closes != 1 {
		t.Fatalf("mDNS Close calls = %d, want 1", mdnsService.closes)
	}
	if mdnsService.done == nil {
		t.Fatal("mDNS lifecycle context was not provided")
	}
	select {
	case <-mdnsService.done:
	default:
		t.Fatal("mDNS lifecycle context was not canceled after Stop")
	}
	if err := node.Start(context.Background(), bus); !errors.Is(err, ErrP2PNodeStopped) {
		t.Fatalf("Start after Stop error = %v, want ErrP2PNodeStopped", err)
	}
	bus.Close()
}

func TestP2PNode_StopHonorsContextWhileCleanupContinues(t *testing.T) {
	node := newLifecycleTestNode(t, false)
	mdnsService := &lifecycleTestDiscovery{}
	node.mdnsFactory = func(done <-chan struct{}, _ discovery.PeerHandler, _ discovery.PeerHandler) peerDiscoveryService {
		mdnsService.done = done
		return mdnsService
	}

	if err := node.Start(context.Background(), events.New()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	releaseWork := make(chan struct{})
	if !node.startBackground(func() { <-releaseWork }) {
		t.Fatal("startBackground rejected work before shutdown")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := node.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error = %v, want context deadline exceeded", err)
	}
	if err := node.Start(context.Background(), events.New()); !errors.Is(err, ErrP2PNodeStopped) {
		t.Fatalf("Start during cleanup error = %v, want ErrP2PNodeStopped", err)
	}

	close(releaseWork)
	if err := node.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after background work exits: %v", err)
	}
}

// TestRunTickerLoop_FiresOnSchedule verifies the ticker loop calls its work
// function once per interval and exits when the context is canceled, using
// synctest's virtual clock so no real 30s sleep is needed.
func TestRunTickerLoop_FiresOnSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())

		var calls atomic.Int32
		go runTickerLoop(ctx, 30*time.Second, func() { calls.Add(1) })

		// Advance the virtual clock past two intervals.
		time.Sleep(61 * time.Second)
		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Fatalf("calls after 61s = %d, want 2", got)
		}

		// Cancellation must stop the loop; no further ticks fire.
		cancel()
		synctest.Wait()
		time.Sleep(30 * time.Second)
		synctest.Wait()
		if got := calls.Load(); got != 2 {
			t.Fatalf("calls after cancel = %d, want still 2", got)
		}
	})
}

// TestRunTickerLoop_ImmediateExit verifies a pre-canceled context exits the
// loop without firing any work.
func TestRunTickerLoop_ImmediateExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		var calls atomic.Int32
		go runTickerLoop(ctx, time.Second, func() { calls.Add(1) })

		synctest.Wait()
		if got := calls.Load(); got != 0 {
			t.Fatalf("calls = %d, want 0 for pre-canceled context", got)
		}
	})
}
