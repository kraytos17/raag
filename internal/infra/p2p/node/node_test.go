package node

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
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/events"
	p2p "github.com/p-society/raag/internal/infra/p2p"
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

func newLifecycleTestNode(t *testing.T, broadcastEnabled bool) *Node {
	t.Helper()
	privateKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateEd25519Key: %v", err)
	}
	gater := p2p.NewPeerGater()
	gater.SetLANOnly(true)
	libp2pHost, err := p2p.NewHost(privateKey, p2p.P2PConfig{
		ListenAddrs: []string{testLoopbackListenAddr}, LANOnly: true, ConnectionGater: gater,
	})
	if err != nil {
		t.Fatalf("p2p.NewHost: %v", err)
	}
	node, err := NewWithHost(Config{
		MaxKnownPeers: 10, PeerDataTTL: time.Hour, MdnsServiceName: testMdnsServiceName, LANOnly: true, BroadcastEnabled: broadcastEnabled,
	}, mocks.NewMockLibraryRepository(), gater, libp2pHost)
	if err != nil {
		_ = libp2pHost.Close()
		t.Fatalf("NewWithHost: %v", err)
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
	if err := node.Start(context.Background(), nil); !errors.Is(err, ErrNodeStopped) {
		t.Fatalf("Start after failed startup error = %v, want ErrNodeStopped", err)
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
	if err := node.Start(context.Background(), bus); !errors.Is(err, ErrNodeStopped) {
		t.Fatalf("Start after Stop error = %v, want ErrNodeStopped", err)
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
	if err := node.Start(context.Background(), events.New()); !errors.Is(err, ErrNodeStopped) {
		t.Fatalf("Start during cleanup error = %v, want ErrNodeStopped", err)
	}

	close(releaseWork)
	if err := node.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after background work exits: %v", err)
	}
}

func TestConnNotifier_MultipleConnectionsShareOneAdmissionSlot(t *testing.T) {
	mn := mocknet.New()
	defer mn.Close()
	target, err := mn.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer target: %v", err)
	}
	peerA := peer.ID("admission-peer-a")
	peerB := peer.ID("admission-peer-b")

	peerMgr := newPeerLifecycle(t, target)
	notifier := &connNotifier{
		host:      target,
		peerMgr:   peerMgr,
		bus:       events.New(),
		admission: protocols.NewAdmissionRegistry(),
		maxPeers:  1,
	}
	defer notifier.bus.Close()
	disconnectedEvents := make(chan peer.ID, 16)
	unsubscribe := notifier.bus.Subscribe(domain.EventPeerDisconnected, func(event domain.Event) {
		if payload, ok := event.Payload.(domain.PeerDisconnectedPayload); ok {
			disconnectedEvents <- payload.PeerID
		}
	})
	defer unsubscribe()

	connA1 := newNotifierTestConn(peerA)
	connA2 := newNotifierTestConn(peerA)
	notifier.Connected(nil, connA1)
	notifier.Connected(nil, connA2)
	if got := notifier.peerConnections[peerA]; got != 2 {
		t.Fatalf("peerConnections[%s] = %d, want 2", peerA, got)
	}
	if got := len(notifier.peerConnections); got != 1 {
		t.Fatalf("connected peers = %d, want 1 (one slot held)", got)
	}

	// The single admission slot is taken: a second peer must be rejected
	// without tracking it, and its (rejected) connection teardown must not
	// publish a disconnect event for a peer that was never admitted.
	connB := newNotifierTestConn(peerB)
	notifier.Connected(nil, connB)
	if _, ok := notifier.peerConnections[peerB]; ok {
		t.Fatalf("rejected peer %s must not consume a connection count", peerB)
	}
	notifier.Disconnected(nil, connB)
	assertNoDisconnectEvent(t, disconnectedEvents, peerB)

	// Dropping one of peerA's connections keeps the slot held: no
	// disconnect event, still one connected peer.
	notifier.Disconnected(nil, connA1)
	if got := notifier.peerConnections[peerA]; got != 1 {
		t.Fatalf("peerConnections[%s] = %d, want 1 after first disconnect", peerA, got)
	}
	if got := len(notifier.peerConnections); got != 1 {
		t.Fatalf("connected peers = %d, want 1 (slot still held)", got)
	}
	assertNoDisconnectEvent(t, disconnectedEvents, peerA)

	// Dropping the last connection releases the slot with exactly one
	// disconnect event.
	notifier.Disconnected(nil, connA2)
	waitForPeerEvent(t, disconnectedEvents, peerA)
	if _, ok := notifier.peerConnections[peerA]; ok {
		t.Fatalf("peer %s must have no connection count after its last disconnect", peerA)
	}
	if got := len(notifier.peerConnections); got != 0 {
		t.Fatalf("connected peers = %d, want 0 (slot released)", got)
	}
}

// notifierTestConn is a minimal network.Conn carrying a remote peer ID and a
// dialable multiaddr for tests that drive connNotifier directly.
type notifierTestConn struct {
	remote peer.ID
	closed atomic.Bool
}

func newNotifierTestConn(remote peer.ID) *notifierTestConn {
	return &notifierTestConn{remote: remote}
}

func (c *notifierTestConn) Close() error { c.closed.Store(true); return nil }
func (c *notifierTestConn) CloseWithError(_ network.ConnErrorCode) error {
	c.closed.Store(true)
	return nil
}
func (c *notifierTestConn) ID() string { return "notifier-test-conn" }
func (c *notifierTestConn) NewStream(context.Context) (network.Stream, error) {
	return nil, errNotifierTestConnNoStream
}
func (c *notifierTestConn) GetStreams() []network.Stream { return nil }
func (c *notifierTestConn) IsClosed() bool               { return c.closed.Load() }
func (c *notifierTestConn) As(any) bool                  { return false }
func (c *notifierTestConn) LocalPeer() peer.ID           { return peer.ID("local") }
func (c *notifierTestConn) RemotePeer() peer.ID          { return c.remote }
func (c *notifierTestConn) RemotePublicKey() crypto.PubKey {
	return nil
}

func (c *notifierTestConn) ConnState() network.ConnectionState {
	return network.ConnectionState{}
}

func (c *notifierTestConn) LocalMultiaddr() multiaddr.Multiaddr {
	return notifierTestAddr
}

func (c *notifierTestConn) RemoteMultiaddr() multiaddr.Multiaddr {
	return notifierTestAddr
}
func (c *notifierTestConn) Stat() network.ConnStats  { return network.ConnStats{} }
func (c *notifierTestConn) Scope() network.ConnScope { return nil }

var (
	notifierTestAddr            = multiaddr.StringCast("/ip4/127.0.0.1/tcp/4001")
	errNotifierTestConnNoStream = errors.New("notifierTestConn: no streams")
)

func assertNoDisconnectEvent(t *testing.T, events <-chan peer.ID, pid peer.ID) {
	t.Helper()
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case got := <-events:
			if got == pid {
				t.Fatalf("unexpected disconnect event for %s", pid)
			}
		case <-timer.C:
			return
		}
	}
}

// testLoopbackListenAddr is the loopback wildcard used for constructing real
// hosts in constructor tests.
const testLoopbackListenAddr = "/ip4/127.0.0.1/tcp/0"

// testMdnsServiceName is the mDNS service name used in constructor tests.
const testMdnsServiceName = "raag-test"

func makeTestHost(t *testing.T) host.Host {
	t.Helper()
	privateKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateEd25519Key: %v", err)
	}
	h, err := p2p.NewHost(privateKey, p2p.P2PConfig{ListenAddrs: []string{testLoopbackListenAddr}})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func newPeerLifecycle(t *testing.T, h host.Host) p2p.PeerLifecycle {
	t.Helper()
	return p2p.NewPeerLifecycle(h, discovery.NewPeerCache(10), nil, p2p.NewPeerScorer(), time.Hour, nil)
}

func TestNewWithHost_WiresComponents(t *testing.T) {
	gater := p2p.NewPeerGater()
	n, err := NewWithHost(Config{MaxPeers: 5, MaxKnownPeers: 10, PeerDataTTL: time.Hour, MdnsServiceName: testMdnsServiceName}, nil, gater, makeTestHost(t))
	if err != nil {
		t.Fatalf("NewWithHost() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := n.Stop(ctx); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})
	for name, got := range map[string]any{
		"host": n.Host(), "gater": n.PeerGater(), "streamPool": n.StreamPool(),
		"streamHandler": n.StreamHandler(), "syncHandler": n.SyncHandler(),
		"resolver": n.Resolver(), "scorer": n.scorer, "peerCache": n.PeerCache(),
		"peerMgr": n.peerMgr, "admission": n.admission,
	} {
		if got == nil {
			t.Errorf("%s = nil, want wired component", name)
		}
	}
	if n.ID() != n.Host().ID() {
		t.Errorf("ID() = %s, want host ID %s", n.ID(), n.Host().ID())
	}
}

func TestNewWithHost_LANOnlyDropsBootstrapPeers(t *testing.T) {
	gater := p2p.NewPeerGater()
	n, err := NewWithHost(Config{LANOnly: true, BootstrapPeers: []string{"/ip4/10.0.0.1/tcp/7844/p2p/QmFoo"}, MaxKnownPeers: 10, MdnsServiceName: testMdnsServiceName}, nil, gater, makeTestHost(t))
	if err != nil {
		t.Fatalf("NewWithHost() error = %v", err)
	}
	t.Cleanup(func() { _ = n.Stop(context.Background()) })
	if len(n.bootstrapPeers) != 0 {
		t.Errorf("bootstrapPeers = %v, want nil in LAN-only mode", n.bootstrapPeers)
	}
	if !n.lanOnly {
		t.Error("lanOnly = false, want true")
	}
}

func TestConnNotifier_ConcurrentConnectionsRespectMaxPeers(t *testing.T) {
	mn := mocknet.New()
	defer mn.Close()
	target, err := mn.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer target: %v", err)
	}
	peers := make([]host.Host, 3)
	for i := range peers {
		peers[i], err = mn.GenPeer()
		if err != nil {
			t.Fatalf("GenPeer %d: %v", i, err)
		}
	}
	if err := mn.LinkAll(); err != nil {
		t.Fatalf("LinkAll: %v", err)
	}

	peerMgr := newPeerLifecycle(t, target)
	notifier := &connNotifier{
		host:      target,
		peerMgr:   peerMgr,
		bus:       events.New(),
		admission: protocols.NewAdmissionRegistry(),
		maxPeers:  1,
	}
	defer notifier.bus.Close()
	disconnectedEvents := make(chan peer.ID, 16)
	unsubscribe := notifier.bus.Subscribe(domain.EventPeerDisconnected, func(event domain.Event) {
		if payload, ok := event.Payload.(domain.PeerDisconnectedPayload); ok {
			disconnectedEvents <- payload.PeerID
		}
	})
	defer unsubscribe()
	target.Network().Notify(notifier)

	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, len(peers))
	for _, remote := range peers {
		wg.Go(func() {
			<-start
			errs <- remote.Connect(context.Background(), target.Peerstore().PeerInfo(target.ID()))
		})
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("Connect: %v", err)
		}
	}

	connected := waitForConnectedPeerCount(t, target, 1)
	if len(connected) != 1 {
		ids := make([]string, 0, len(connected))
		for _, id := range connected {
			ids = append(ids, id.String())
		}
		t.Fatalf("connected peers = %v, want exactly one peer with maxPeers=1", ids)
	}

	accepted := connected[0]
	var replacement host.Host
	for _, candidate := range peers {
		if candidate.ID() != accepted {
			replacement = candidate
			break
		}
	}
	if replacement == nil {
		t.Fatal("no unaccepted peer available for admission after disconnect")
	}
	if err := target.Network().ClosePeer(accepted); err != nil {
		t.Fatalf("ClosePeer(%s): %v", accepted, err)
	}
	waitForPeerEvent(t, disconnectedEvents, accepted)
	deadline := time.Now().Add(time.Second)
	for target.Network().Connectedness(replacement.ID()) != network.Connected && time.Now().Before(deadline) {
		if err := target.Connect(context.Background(), replacement.Peerstore().PeerInfo(replacement.ID())); err != nil {
			t.Fatalf("Connect replacement peer after admission slot freed: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	if target.Network().Connectedness(replacement.ID()) != network.Connected {
		t.Fatalf("replacement peer %s was not admitted after slot freed", replacement.ID())
	}
}

func waitForConnectedPeerCount(t *testing.T, h host.Host, want int) []peer.ID {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	peers := h.Network().Peers()
	for len(peers) != want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		peers = h.Network().Peers()
	}
	return peers
}

func waitForPeerEvent(t *testing.T, events <-chan peer.ID, want peer.ID) {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case pid := <-events:
			if pid == want {
				return
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for peer event for %s", want)
		}
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
