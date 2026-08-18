package discovery

import (
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	pb "github.com/p-society/raag/proto/gen"
	"google.golang.org/protobuf/proto"
)

// freeUDPPort returns a currently-free UDP port on the loopback interface.
func freeUDPPort(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("freeUDPPort: %v", err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func TestConnectedPeerToAddrInfo_RoundTrip(t *testing.T) {
	_, host := newMocknetHost(t)
	defer host.Close()

	msg := &pb.ConnectedPeer{
		PeerId: host.ID().String(),
		Addrs:  multiaddrStrings(host.Addrs()),
	}
	pi, err := connectedPeerToAddrInfo(msg)
	if err != nil {
		t.Fatalf("connectedPeerToAddrInfo: %v", err)
	}
	if pi.ID != host.ID() {
		t.Errorf("ID = %v, want %v", pi.ID, host.ID())
	}
	if len(pi.Addrs) != len(msg.Addrs) {
		t.Fatalf("addrs len = %d, want %d", len(pi.Addrs), len(msg.Addrs))
	}
	for i, a := range pi.Addrs {
		if a.String() != msg.Addrs[i] {
			t.Errorf("addr %d = %s, want %s", i, a.String(), msg.Addrs[i])
		}
	}
}

func TestConnectedPeerToAddrInfo_BadID(t *testing.T) {
	msg := &pb.ConnectedPeer{PeerId: "not-a-valid-peer-id", Addrs: []string{"/ip4/127.0.0.1/tcp/7844"}}
	if _, err := connectedPeerToAddrInfo(msg); err == nil {
		t.Fatal("expected error for invalid peer ID")
	}
}

func TestConnectedPeerToAddrInfo_SkipsBadAddrs(t *testing.T) {
	msg := &pb.ConnectedPeer{
		PeerId: "12D3KooWDpJ7As7BWAwRMfu1VU2WCqNjvq387JEYKDBj4kx6nXTN",
		Addrs:  []string{"/ip4/127.0.0.1/tcp/7844", "garbage-multiaddr"},
	}
	pi, err := connectedPeerToAddrInfo(msg)
	if err != nil {
		t.Fatalf("connectedPeerToAddrInfo: %v", err)
	}
	if len(pi.Addrs) != 1 {
		t.Fatalf("addrs len = %d, want 1 (invalid multiaddr skipped)", len(pi.Addrs))
	}
}

// newMocknetHost creates a mocknet and returns it plus a fresh host.
func newMocknetHost(t *testing.T) (mocknet.Mocknet, host.Host) {
	t.Helper()
	net := mocknet.New()
	host, err := net.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer: %v", err)
	}
	return net, host
}

func TestBroadcastDiscovery_AnnouncesAndDiscovers(t *testing.T) {
	mn := mocknet.New()
	defer mn.Close()
	hostA, err := mn.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer A: %v", err)
	}

	hostB, err := mn.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer B: %v", err)
	}
	if err := mn.LinkAll(); err != nil {
		t.Fatalf("LinkAll: %v", err)
	}

	// A announces to B's listener; B announces to A's listener. Real UDP on
	// loopback for discovery, connect path over the shared mocknet.
	portA := freeUDPPort(t)
	portB := freeUDPPort(t)

	connectedCh := make(chan peer.ID, 4)
	mk := func(h host.Host, listenPort, targetPort int, done <-chan struct{}) *BroadcastDiscovery {
		bc := NewBroadcastDiscovery(
			done, h, listenPort,
			func(pi peer.AddrInfo) {},
			func(pi peer.AddrInfo) { connectedCh <- pi.ID },
		)
		bc.target = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: targetPort}
		bc.interval = 50 * time.Millisecond
		return bc
	}

	doneA := make(chan struct{})
	doneB := make(chan struct{})
	bcA := mk(hostA, portA, portB, doneA)
	bcB := mk(hostB, portB, portA, doneB)

	if err := bcA.Start(); err != nil {
		t.Fatalf("Start A: %v", err)
	}
	defer bcA.Close()
	if err := bcB.Start(); err != nil {
		t.Fatalf("Start B: %v", err)
	}
	defer bcB.Close()

	// Each side should eventually discover (and connect to) the other.
	wantA, wantB := hostB.ID(), hostA.ID()
	gotA, gotB := false, false
	deadline := time.After(5 * time.Second)
	for !gotA || !gotB {
		select {
		case id := <-connectedCh:
			if id == wantA {
				gotA = true
			}
			if id == wantB {
				gotB = true
			}
		case <-deadline:
			t.Fatalf("timed out: discovered A=%v, B=%v", gotB, gotA)
		}
	}
}

func TestBroadcastDiscovery_SkipsSelf(t *testing.T) {
	mn := mocknet.New()
	defer mn.Close()
	host, err := mn.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer: %v", err)
	}

	port := freeUDPPort(t)
	done := make(chan struct{})
	bc := NewBroadcastDiscovery(
		done, host, port,
		func(pi peer.AddrInfo) {},
		func(pi peer.AddrInfo) { t.Error("self announcement should not connect") },
	)

	bc.target = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	bc.interval = 50 * time.Millisecond
	if err := bc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer bc.Close()

	// Feed the node's own announcement through the listener path: the Notifee
	// must drop it (self skip) rather than queue it for connection.
	bc.notifee.HandlePeerFound(host.Peerstore().PeerInfo(host.ID()))
	<-time.After(200 * time.Millisecond)
	if len(bc.notifee.pending) != 0 {
		t.Fatalf("pending queue = %d, want 0 (self must be skipped)", len(bc.notifee.pending))
	}
}

func TestBroadcastDiscovery_IgnoresGarbage(t *testing.T) {
	mn := mocknet.New()
	defer mn.Close()
	host, err := mn.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer: %v", err)
	}

	port := freeUDPPort(t)
	done := make(chan struct{})
	bc := NewBroadcastDiscovery(done, host, port, nil, nil)
	bc.target = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	bc.interval = 50 * time.Millisecond
	if err := bc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer bc.Close()

	// Fire random bytes at the listener; it must be skipped without panicking.
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte{0xff, 0x00, 0xde, 0xad, 0xbe, 0xef}); err != nil {
		t.Fatalf("Write garbage: %v", err)
	}

	<-time.After(200 * time.Millisecond)
	if len(bc.notifee.pending) != 0 {
		t.Fatalf("pending queue = %d, want 0 (garbage ignored)", len(bc.notifee.pending))
	}
}

func TestBroadcastDiscovery_CloseStopsGoroutines(t *testing.T) {
	mn := mocknet.New()
	defer mn.Close()
	host, err := mn.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer: %v", err)
	}

	port := freeUDPPort(t)
	done := make(chan struct{})
	bc := NewBroadcastDiscovery(done, host, port, nil, nil)
	bc.target = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	bc.interval = 10 * time.Millisecond
	if err := bc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := bc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// A second Close must be a no-op, not an error or panic.
	if err := bc.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	close(done)
}

// TestBroadcastDiscovery_SharedPort verifies two nodes on one host can bind
// the same broadcast port (SO_REUSEADDR/SO_REUSEPORT), guarding against the
// EADDRINUSE regression that a second instance would otherwise hit.
func TestBroadcastDiscovery_SharedPort(t *testing.T) {
	mn := mocknet.New()
	defer mn.Close()
	hostA, err := mn.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer A: %v", err)
	}

	hostB, err := mn.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer B: %v", err)
	}

	port := freeUDPPort(t)
	bcA := NewBroadcastDiscovery(make(chan struct{}), hostA, port, nil, nil)
	bcB := NewBroadcastDiscovery(make(chan struct{}), hostB, port, nil, nil)
	defer bcA.Close()
	defer bcB.Close()

	if err := bcA.Start(); err != nil {
		t.Fatalf("Start A: %v", err)
	}
	if err := bcB.Start(); err != nil {
		t.Fatalf("Start B (same port) should succeed with SO_REUSEPORT: %v", err)
	}
}

func TestBroadcastDiscovery_MalformedDatagramNoPanic(t *testing.T) {
	mn := mocknet.New()
	defer mn.Close()
	host, err := mn.GenPeer()
	if err != nil {
		t.Fatalf("GenPeer: %v", err)
	}

	port := freeUDPPort(t)
	bc := NewBroadcastDiscovery(make(chan struct{}), host, port, nil, nil)
	bc.target = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	bc.interval = 10 * time.Millisecond
	if err := bc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer bc.Close()

	// A ConnectedPeer proto with an empty peer ID must be dropped.
	payload, err := proto.Marshal(&pb.ConnectedPeer{PeerId: ""})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatalf("DialUDP: %v", err)
	}

	defer conn.Close()
	if _, err := conn.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}

	<-time.After(200 * time.Millisecond)
	if len(bc.notifee.pending) != 0 {
		t.Fatalf("pending queue = %d, want 0 (empty peer dropped)", len(bc.notifee.pending))
	}
}
