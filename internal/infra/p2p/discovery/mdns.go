package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
)

type PeerHandler func(peer.AddrInfo)

type Notifee struct {
	host         host.Host
	onDiscovered PeerHandler
	onConnected  PeerHandler
	mu           sync.Mutex
	done         <-chan struct{}
	sem          chan struct{}
}

func (n *Notifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.host.ID() {
		return
	}
	if n.host.Network().Connectedness(pi.ID) == network.Connected {
		return
	}

	select {
	case <-n.done:
		return
	default:
	}

	n.mu.Lock()
	onDiscovered := n.onDiscovered
	onConnected := n.onConnected
	n.mu.Unlock()

	if onDiscovered != nil {
		onDiscovered(pi)
	}
	select {
	case n.sem <- struct{}{}:
		go func() {
			defer func() { <-n.sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := n.host.Connect(ctx, pi); err != nil {
				slog.Warn("mDNS: peer connection failed", "peer", pi.ID, "err", err)
				return
			}

			slog.Info("mDNS: peer discovered and connected", "peer", pi.ID)
			if onConnected != nil {
				onConnected(pi)
			}
		}()
	default:
		slog.Debug("mDNS: connection semaphore full, skipping peer", "peer", pi.ID)
	}
}

type MdnsDiscovery struct {
	host        host.Host
	service     mdns.Service
	notifee     *Notifee
	mu          sync.Mutex
	started     bool
	serviceName string
	done        chan struct{}
}

func NewMdnsDiscovery(done <-chan struct{}, h host.Host, serviceName string, onPeer PeerHandler) *MdnsDiscovery {
	n := &Notifee{host: h, onConnected: onPeer, done: done}
	return &MdnsDiscovery{
		host:        h,
		notifee:     n,
		serviceName: serviceName,
		done:        make(chan struct{}),
	}
}

func NewMdnsDiscoveryWithHandlers(done <-chan struct{}, h host.Host, serviceName string, onDiscovered PeerHandler, onConnected PeerHandler) *MdnsDiscovery {
	n := &Notifee{host: h, onDiscovered: onDiscovered, onConnected: onConnected, done: done, sem: make(chan struct{}, 10)}
	return &MdnsDiscovery{
		host:        h,
		notifee:     n,
		serviceName: serviceName,
		done:        make(chan struct{}),
	}
}

func (m *MdnsDiscovery) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}
	if m.service == nil {
		m.service = mdns.NewMdnsService(m.host, m.serviceName, m.notifee)
	}
	if err := m.service.Start(); err != nil {
		return err
	}

	m.started = true
	slog.Info("mDNS discovery started", "service_tag", m.serviceName)
	return nil
}

func (m *MdnsDiscovery) Close() error {
	close(m.done)

	m.mu.Lock()
	svc := m.service
	m.mu.Unlock()
	if svc == nil {
		return nil
	}
	return svc.Close()
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
