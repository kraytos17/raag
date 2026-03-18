package network

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/logger"
)

type RaagConnectionGater struct {
	mu           sync.RWMutex
	blockedPeers map[peer.ID]struct{}
}

func NewRaagConnectionGater() *RaagConnectionGater {
	return &RaagConnectionGater{
		blockedPeers: make(map[peer.ID]struct{}),
	}
}

func (g *RaagConnectionGater) BlockPeer(p peer.ID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.blockedPeers[p] = struct{}{}
	logger.Debugf("Gater: blocked peer %s", p)
}

func (g *RaagConnectionGater) UnblockPeer(p peer.ID) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.blockedPeers, p)
	logger.Debugf("Gater: unblocked peer %s", p)
}

func (g *RaagConnectionGater) IsBlocked(p peer.ID) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, blocked := g.blockedPeers[p]
	return blocked
}

func (g *RaagConnectionGater) InterceptPeerDial(p peer.ID) (allow bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if _, blocked := g.blockedPeers[p]; blocked {
		logger.Debugf("Gater: blocked outbound dial to %s", p)
		return false
	}
	return true
}

func (g *RaagConnectionGater) InterceptAddrDial(p peer.ID, _ multiaddr.Multiaddr) (allow bool) {
	return g.InterceptPeerDial(p)
}

func (g *RaagConnectionGater) InterceptAccept(_ network.ConnMultiaddrs) (allow bool) {
	return true
}

func (g *RaagConnectionGater) InterceptSecured(_ network.Direction, p peer.ID, _ network.ConnMultiaddrs) (allow bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if _, blocked := g.blockedPeers[p]; blocked {
		logger.Warnf("Gater: rejected secured connection from blocked peer %s", p)
		return false
	}
	return true
}

func (g *RaagConnectionGater) InterceptUpgraded(conn network.Conn) (allow bool, reason control.DisconnectReason) {
	return true, 0
}
