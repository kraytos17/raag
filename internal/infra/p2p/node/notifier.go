package node

import (
	"context"
	"log/slog"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/domain"
	p2p "github.com/p-society/raag/internal/infra/p2p"
	protocols "github.com/p-society/raag/internal/infra/p2p/protocols"
)

type connNotifier struct {
	ctx             context.Context
	host            host.Host
	streamHandler   *protocols.StreamHandler
	syncHandler     *protocols.SyncHandler
	peerMgr         p2p.PeerLifecycle
	bus             domain.EventBus
	streamPool      *p2p.StreamPool
	admission       *protocols.AdmissionRegistry
	maxPeers        int
	startBackground func(func()) bool
	connsMu         sync.Mutex
	peerConnections map[peer.ID]int
}

func (cn *connNotifier) Connected(_ network.Network, conn network.Conn) {
	pid := conn.RemotePeer()
	if cn.peerMgr.IsBanned(pid) {
		slog.Warn("rejecting connection from banned peer", "peer", pid)
		if err := conn.Close(); err != nil {
			slog.Debug("failed to close banned peer connection", "peer", pid, "err", err)
		}
		return
	}

	cn.connsMu.Lock()
	if cn.peerConnections == nil {
		cn.peerConnections = make(map[peer.ID]int)
	}
	if cn.peerConnections[pid] == 0 {
		if cn.maxPeers > 0 && len(cn.peerConnections) >= cn.maxPeers {
			cn.connsMu.Unlock()
			slog.Warn("peer connection rejected: max peers reached", "peer", pid, "max_peers", cn.maxPeers)
			if err := conn.Close(); err != nil {
				slog.Debug("failed to close excess peer connection", "peer", pid, "err", err)
			}
			return
		}
	}

	cn.peerConnections[pid]++
	duplicateConnection := cn.peerConnections[pid] > 1
	cn.connsMu.Unlock()
	if duplicateConnection {
		return
	}

	pi := cn.host.Peerstore().PeerInfo(pid)
	cn.peerMgr.CachePeer(pi)
	// Admission is intentionally NOT granted here. A peer becomes admitted only
	// after it completes manifest exchange (SyncHandler.Handle grants it on a
	// ManifestRequest). Granting it eagerly on connect would make the chunk /
	// track-detail / remote-search guards meaningless.
	slog.Debug("peer connected", "peer", pid, "addr", conn.RemoteMultiaddr().String())
	cn.peerMgr.Persist(pid)
	if cn.startBackground != nil {
		cn.startBackground(func() { cn.peerMgr.FetchPeerData(cn.ctx, pid) })
	}

	cn.bus.Publish(context.Background(), domain.NewEvent(
		domain.EventPeerConnected,
		domain.PeerConnectedPayload{
			PeerID:       pid,
			Addr:         conn.RemoteMultiaddr().String(),
			Capabilities: cn.peerMgr.GetPeerCapabilities(pid),
		},
	))
}

func (cn *connNotifier) Disconnected(_ network.Network, conn network.Conn) {
	pid := conn.RemotePeer()
	cn.connsMu.Lock()
	remaining, ok := cn.peerConnections[pid]
	if !ok {
		// Untracked connection: rejected, banned, or never counted. Nothing
		// was started for it, so there is nothing to tear down and no event
		// to publish.
		cn.connsMu.Unlock()
		return
	}

	remaining--
	if remaining <= 0 {
		delete(cn.peerConnections, pid)
	} else {
		cn.peerConnections[pid] = remaining
	}

	cn.connsMu.Unlock()
	if remaining > 0 {
		return
	}

	slog.Debug("peer disconnected", "peer", pid)
	cn.admission.Revoke(pid)
	cn.peerMgr.OnPeerDisconnected(pid)
	cn.peerMgr.Persist(pid)
	if cn.streamPool != nil {
		cn.streamPool.DrainPeer(pid)
	}

	cn.bus.Publish(context.Background(), domain.NewEvent(
		domain.EventPeerDisconnected,
		domain.PeerDisconnectedPayload{PeerID: pid},
	))
}

func (cn *connNotifier) Listen(_ network.Network, addr ma.Multiaddr) {}

func (cn *connNotifier) ListenClose(_ network.Network, addr ma.Multiaddr) {}

func (cn *connNotifier) OpenedStream(_ network.Network, s network.Stream) {}

func (cn *connNotifier) ClosedStream(_ network.Network, s network.Stream) {}
