package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

type PeerHandler func(peer.AddrInfo)

type MdnsDiscovery struct {
	host       host.Host
	handlePeer PeerHandler
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	started    bool
	mu         sync.Mutex
}

func NewMdnsDiscovery(h host.Host, onPeer PeerHandler) *MdnsDiscovery {
	ctx, cancel := context.WithCancel(context.Background())
	return &MdnsDiscovery{
		host:       h,
		handlePeer: onPeer,
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (m *MdnsDiscovery) Start() error {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return nil
	}

	m.started = true
	m.mu.Unlock()

	m.wg.Add(1)
	go m.scanPeers()
	return nil
}

func (m *MdnsDiscovery) scanPeers() {
	defer m.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.discoverPeers()
		}
	}
}

func (m *MdnsDiscovery) discoverPeers() {
	peers := m.host.Network().Peers()
	for _, pid := range peers {
		if pid == m.host.ID() {
			continue
		}

		conns := m.host.Network().ConnsToPeer(pid)
		if len(conns) == 0 {
			continue
		}
		pi := peer.AddrInfo{
			ID:    pid,
			Addrs: []ma.Multiaddr{conns[0].RemoteMultiaddr()},
		}

		m.mu.Lock()
		handle := m.handlePeer
		ctx := m.ctx
		m.mu.Unlock()

		if handle == nil || ctx.Err() != nil {
			return
		}

		slog.Debug("discovered peer", "peer", pid)
		handle(pi)
	}
}

func (m *MdnsDiscovery) Close() error {
	m.mu.Lock()
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}

type PeerCache struct {
	mu      sync.RWMutex
	peers   map[peer.ID]peer.AddrInfo
	maxSize int
}

func NewPeerCache(maxSize int) *PeerCache {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &PeerCache{
		peers:   make(map[peer.ID]peer.AddrInfo),
		maxSize: maxSize,
	}
}

func (c *PeerCache) Add(pi peer.AddrInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.peers) >= c.maxSize {
		return
	}
	c.peers[pi.ID] = pi
}

func (c *PeerCache) Get(id peer.ID) (peer.AddrInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	pi, ok := c.peers[id]
	return pi, ok
}

func (c *PeerCache) Remove(id peer.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.peers, id)
}

func (c *PeerCache) All() []peer.AddrInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]peer.AddrInfo, 0, len(c.peers))
	for _, pi := range c.peers {
		result = append(result, pi)
	}
	return result
}

func (c *PeerCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.peers)
}
