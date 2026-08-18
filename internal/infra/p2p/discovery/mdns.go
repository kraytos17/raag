package discovery

import (
	"context"
	"log/slog"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/p-society/raag/internal/infra/backoff"
)

type PeerHandler func(peer.AddrInfo)

const (
	// pendingQueueSize bounds how many discovered peers can wait for a connect
	// slot before being dropped (only when the queue itself is full).
	pendingQueueSize = 100
	// mdnsConnectRetries is how many times a failed peer connect is retried.
	mdnsConnectRetries = 3
	// mdnsConnectRetryStep is the base backoff between retries.
	mdnsConnectRetryStep = 100 * time.Millisecond
	// MdnsDiscoveryTTL is how long discovered-but-not-connected peers stay in
	// the discovery cache before expiring.
	MdnsDiscoveryTTL = 5 * time.Minute
)

type Notifee struct {
	host         host.Host
	onDiscovered PeerHandler
	onConnected  PeerHandler
	mu           sync.Mutex
	done         <-chan struct{}
	sem          chan struct{}
	pending      chan peer.AddrInfo
	workersOnce  sync.Once
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
	n.mu.Unlock()

	if onDiscovered != nil {
		onDiscovered(pi)
	}

	// Queue the peer for connection; the worker pool drains pending and
	// connects with retry. Only drop when the queue itself is full.
	select {
	case n.pending <- pi:
	default:
		slog.Debug("mDNS: pending connect queue full, dropping peer", "peer", pi.ID)
	}
}

// startWorkers launches the connection worker pool. It must be called after
// NewMdnsDiscoveryWithHandlers, from MdnsDiscovery.Start.
func (n *Notifee) startWorkers() {
	n.workersOnce.Do(func() {
		go n.workerLoop()
	})
}

func (n *Notifee) workerLoop() {
	for {
		select {
		case <-n.done:
			return
		case pi := <-n.pending:
			select {
			case n.sem <- struct{}{}:
				go func() {
					defer func() { <-n.sem }()
					n.connectWithRetry(pi)
				}()
			case <-n.done:
				return
			}
		}
	}
}

// connectWithRetry attempts to connect to a peer, retrying with a linear
// backoff on failure.
func (n *Notifee) connectWithRetry(pi peer.AddrInfo) {
	if err := n.connect(pi); err == nil {
		n.onConnected(pi)
		return
	}
	for attempt := 1; attempt <= mdnsConnectRetries; attempt++ {
		if backoff.Wait(n.done, backoff.Linear(attempt, mdnsConnectRetryStep)) {
			return
		}
		if err := n.connect(pi); err == nil {
			n.onConnected(pi)
			return
		}
	}
}

func (n *Notifee) connect(pi peer.AddrInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := n.host.Connect(ctx, pi); err != nil {
		slog.Warn("mDNS: peer connection failed", "peer", pi.ID, "err", err)
		return err
	}
	slog.Info("mDNS: peer discovered and connected", "peer", pi.ID)
	return nil
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
	n := &Notifee{
		host:        h,
		onConnected: onPeer,
		done:        done,
		pending:     make(chan peer.AddrInfo, pendingQueueSize),
	}
	return &MdnsDiscovery{
		host:        h,
		notifee:     n,
		serviceName: serviceName,
		done:        make(chan struct{}),
	}
}

func NewMdnsDiscoveryWithHandlers(done <-chan struct{}, h host.Host, serviceName string, onDiscovered PeerHandler, onConnected PeerHandler) *MdnsDiscovery {
	n := &Notifee{
		host:         h,
		onDiscovered: onDiscovered,
		onConnected:  onConnected,
		done:         done,
		sem:          make(chan struct{}, 10),
		pending:      make(chan peer.AddrInfo, pendingQueueSize),
	}
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

	m.notifee.startWorkers()
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

// TTLPeerCache is a discovery peer cache whose entries expire after a TTL, used
// for discovered-but-not-connected peers so they don't persist forever.
type TTLPeerCache struct {
	cache *expirable.LRU[peer.ID, peer.AddrInfo]
}

func NewTTLPeerCache(maxSize int, ttl time.Duration) *TTLPeerCache {
	if maxSize <= 0 {
		maxSize = 100
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &TTLPeerCache{
		cache: expirable.NewLRU[peer.ID, peer.AddrInfo](maxSize, nil, ttl),
	}
}

func (c *TTLPeerCache) Add(pi peer.AddrInfo) {
	c.cache.Add(pi.ID, pi)
}

func (c *TTLPeerCache) Remove(id peer.ID) {
	c.cache.Remove(id)
}

func (c *TTLPeerCache) All() []peer.AddrInfo {
	result := make([]peer.AddrInfo, 0, c.cache.Len())
	for _, info := range c.cache.Keys() {
		if v, ok := c.cache.Get(info); ok {
			result = append(result, v)
		}
	}
	return result
}
