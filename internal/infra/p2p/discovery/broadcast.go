package discovery

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	pb "github.com/p-society/raag/proto/gen"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

const (
	// DefaultBroadcastPort is the UDP port used for the LAN broadcast beacon.
	// It is distinct from the libp2p listen ports (7844) and the stream port
	// (7845) so a beacon listener can coexist with normal P2P traffic.
	DefaultBroadcastPort = 7846
	// broadcastAnnounceInterval is how often the node re-announces itself.
	broadcastAnnounceInterval = 2 * time.Second
)

// BroadcastDiscovery is a discovery fallback: it
// periodically broadcasts the node's own peer.AddrInfo as a pb.ConnectedPeer
// datagram to the LAN broadcast address, and listens for peers announcing the
// same way. Discovered peers flow through the same Notifee connect-with-retry
// pipeline as mDNS, so the two mechanisms dedupe naturally (HandlePeerFound
// skips self and already-connected peers).
//
// Unlike mDNS (multicast to 224.0.0.251:5353), plain UDP broadcast survives
// APs and WiFi drivers that filter multicast — the private-hotspot case.
type BroadcastDiscovery struct {
	host      host.Host
	conn      *net.UDPConn
	port      int
	notifee   *Notifee
	mu        sync.Mutex
	started   bool
	stop      chan struct{}
	closeOnce sync.Once
	done      <-chan struct{}
	interval  time.Duration
	target    *net.UDPAddr
}

func NewBroadcastDiscovery(done <-chan struct{}, h host.Host, port int, onDiscovered PeerHandler, onConnected PeerHandler) *BroadcastDiscovery {
	n := &Notifee{
		host:         h,
		onDiscovered: onDiscovered,
		onConnected:  onConnected,
		done:         done,
		sem:          make(chan struct{}, 10),
		pending:      make(chan peer.AddrInfo, pendingQueueSize),
	}
	return &BroadcastDiscovery{
		host:     h,
		port:     port,
		notifee:  n,
		done:     done,
		stop:     make(chan struct{}),
		interval: broadcastAnnounceInterval,
		target:   &net.UDPAddr{IP: net.IPv4bcast, Port: port},
	}
}

// Start binds the UDP listener and launches the announce + listen loops.
func (b *BroadcastDiscovery) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started {
		return nil
	}

	lc := net.ListenConfig{
		Control: func(networkType, address string, c syscall.RawConn) error {
			var controlErr error
			if err := c.Control(func(fd uintptr) {
				// Allow the socket to send to the broadcast address.
				if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1); err != nil {
					controlErr = err
					return
				}
				if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
					controlErr = err
					return
				}
				controlErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
			}); err != nil {
				return err
			}
			return controlErr
		},
	}

	pc, err := lc.ListenPacket(context.Background(), "udp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(b.port)))
	if err != nil {
		return err
	}

	conn, ok := pc.(*net.UDPConn)
	if !ok {
		pc.Close()
		return errors.New("broadcast: expected *net.UDPConn from ListenPacket")
	}
	b.conn = conn

	go b.announceLoop()
	go b.listenLoop()
	b.notifee.startWorkers()
	b.started = true
	slog.Info("broadcast discovery started", "port", b.port, "target", b.target.String())
	return nil
}

// announceLoop periodically broadcasts the node's own address info so other
// nodes on the LAN can discover and connect to it.
func (b *BroadcastDiscovery) announceLoop() {
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()

	b.announce()
	for {
		select {
		case <-b.done:
			return
		case <-b.stop:
			return
		case <-ticker.C:
			b.announce()
		}
	}
}

func (b *BroadcastDiscovery) announce() {
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()
	if conn == nil {
		return
	}

	msg := &pb.ConnectedPeer{
		PeerId: b.host.ID().String(),
		Addrs:  multiaddrStrings(b.host.Addrs()),
	}

	payload, err := proto.Marshal(msg)
	if err != nil {
		slog.Debug("broadcast: failed to marshal announcement", "error", err)
		return
	}
	if _, err := conn.WriteToUDP(payload, b.target); err != nil {
		slog.Debug("broadcast: announce failed", "error", err)
	}
}

// listenLoop reads broadcast datagrams and connects to announcing peers.
func (b *BroadcastDiscovery) listenLoop() {
	buf := make([]byte, 64*1024)
	for {
		b.mu.Lock()
		conn := b.conn
		b.mu.Unlock()
		if conn == nil {
			return
		}

		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return
		}

		select {
		case <-b.done:
			return
		case <-b.stop:
			return
		default:
		}

		var msg pb.ConnectedPeer
		if err := proto.Unmarshal(buf[:n], &msg); err != nil {
			slog.Debug("broadcast: ignoring malformed datagram", "bytes", n)
			continue
		}
		if msg.PeerId == "" {
			continue
		}

		pi, err := connectedPeerToAddrInfo(&msg)
		if err != nil || len(pi.Addrs) == 0 {
			slog.Debug("broadcast: ignoring unusable announcement", "peer", msg.PeerId, "error", err)
			continue
		}

		select {
		case <-b.done:
			return
		default:
		}
		b.notifee.HandlePeerFound(pi)
	}
}

// Close stops the broadcast discovery: it signals the announce and listen
// loops to exit and closes the UDP socket, which unblocks an in-flight read.
func (b *BroadcastDiscovery) Close() error {
	b.closeOnce.Do(func() { close(b.stop) })
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.conn == nil {
		return nil
	}

	err := b.conn.Close()
	b.conn = nil
	return err
}

// multiaddrStrings renders a slice of multiaddrs as their string form.
func multiaddrStrings(addrs []ma.Multiaddr) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a != nil {
			out = append(out, a.String())
		}
	}
	return out
}

// connectedPeerToAddrInfo converts a pb.ConnectedPeer announcement into a
// peer.AddrInfo, skipping invalid or self addresses.
func connectedPeerToAddrInfo(msg *pb.ConnectedPeer) (peer.AddrInfo, error) {
	id, err := peer.Decode(msg.PeerId)
	if err != nil {
		return peer.AddrInfo{}, err
	}

	pi := peer.AddrInfo{ID: id}
	for _, s := range msg.Addrs {
		a, err := ma.NewMultiaddr(s)
		if err != nil {
			continue
		}
		pi.Addrs = append(pi.Addrs, a)
	}
	return pi, nil
}
