package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

type PeerHandler func(peer.AddrInfo)

type Notifee struct {
	host       host.Host
	handlePeer PeerHandler
	mu         sync.Mutex
}

func (n *Notifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.host.ID() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := n.host.Connect(ctx, pi); err != nil {
		slog.Warn("mDNS: failed to connect to peer", "peer", pi.ID, "err", err)
		return
	}

	n.mu.Lock()
	handler := n.handlePeer
	n.mu.Unlock()

	slog.Info("mDNS: peer discovered and connected", "peer", pi.ID)
	handler(pi)
}

type MdnsDiscovery struct {
	service     mdns.Service
	notifee     *Notifee
	mu          sync.Mutex
	started     bool
	serviceName string
}

func NewMdnsDiscovery(h host.Host, serviceName string, onPeer PeerHandler) *MdnsDiscovery {
	n := &Notifee{host: h, handlePeer: onPeer}
	svc := mdns.NewMdnsService(h, serviceName, n)
	return &MdnsDiscovery{
		service:     svc,
		notifee:     n,
		serviceName: serviceName,
	}
}

func (m *MdnsDiscovery) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}
	if err := m.service.Start(); err != nil {
		return err
	}

	m.started = true
	slog.Info("mDNS discovery started", "service_tag", m.serviceName)
	return nil
}

func (m *MdnsDiscovery) Close() error {
	return m.service.Close()
}

type PeerCache struct {
	mu    sync.RWMutex
	cache *lru.Cache[peer.ID, peer.AddrInfo]
}

func NewPeerCache(maxSize int) *PeerCache {
	if maxSize <= 0 {
		maxSize = 100
	}

	cache, _ := lru.New[peer.ID, peer.AddrInfo](maxSize)
	return &PeerCache{
		cache: cache,
	}
}

func (c *PeerCache) Add(pi peer.AddrInfo) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	evicted := c.cache.Add(pi.ID, pi)
	return !evicted
}

func (c *PeerCache) Get(id peer.ID) (peer.AddrInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.Get(id)
}

func (c *PeerCache) Remove(id peer.ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Remove(id)
}

func (c *PeerCache) All() []peer.AddrInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]peer.AddrInfo, 0, c.cache.Len())
	for _, info := range c.cache.Keys() {
		if v, ok := c.cache.Peek(info); ok {
			result = append(result, v)
		}
	}
	return result
}

func (c *PeerCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cache.Len()
}
