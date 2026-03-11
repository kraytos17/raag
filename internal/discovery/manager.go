package discovery

import (
	"context"
	"fmt"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/logger"
)

type Manager struct {
	host           host.Host
	dht            *dht.IpfsDHT
	discovery      *routing.RoutingDiscovery
	peers          map[peer.ID]*peer.AddrInfo
	mu             sync.RWMutex
	persistence    *PeerPersistence
	tracker        *TrackerClient
	maxPeers       int
	trackerURL     string
	listenHost     string
	networkManager interface {
		NotifyPeerConnected(peerID peer.ID, addr string)
	}
}

func NewManager(h host.Host, trackerURL string, maxPeers int, listenHost string) *Manager {
	return &Manager{
		host:        h,
		peers:       make(map[peer.ID]*peer.AddrInfo),
		persistence: NewPeerPersistence(),
		tracker:     NewTrackerClient(trackerURL),
		maxPeers:    maxPeers,
		trackerURL:  trackerURL,
		listenHost:  listenHost,
	}
}

func (m *Manager) SetNetworkManager(nm interface {
	NotifyPeerConnected(peerID peer.ID, addr string)
},
) {
	m.networkManager = nm
}

func (m *Manager) Start(ctx context.Context) error {
	if err := m.loadPersistedPeers(); err != nil {
		logger.Error("Failed to load persisted peers", "error", err)
	}

	logger.Info("Starting peer discovery from tracker...")
	m.discoverFromTracker(ctx)
	m.registerSelfWithTracker(ctx)

	if m.listenHost == "127.0.0.1" || m.listenHost == "localhost" {
		logger.Info("Skipping mDNS discovery (localhost mode)")
	} else {
		logger.Info("Starting mDNS discovery...")
		m.discoverViaMDNS(ctx)
		logger.Info("mDNS discovery completed")
	}

	logger.Info("Initializing DHT with existing peer connections...")
	if err := m.initDHT(ctx); err != nil {
		logger.Error("Failed to initialize DHT", "error", err)
	}
	m.populateDHTFromConnectedPeers()

	go m.discoverViaDHTRetry(ctx)
	go m.advertisePeriodically(ctx)
	return nil
}

func (m *Manager) registerSelfWithTracker(ctx context.Context) {
	if m.trackerURL == "" {
		return
	}

	time.Sleep(2 * time.Second)
	m.refreshRegistration(ctx)
}

func (m *Manager) refreshRegistration(ctx context.Context) {
	if m.trackerURL == "" {
		return
	}

	addrs := m.host.Addrs()
	if len(addrs) == 0 {
		return
	}

	multiaddr := addrs[0].Encapsulate(multiaddr.StringCast("/p2p/" + m.host.ID().String())).String()
	if err := m.tracker.RegisterPeer(ctx, multiaddr); err != nil {
		logger.Error("Failed to register with tracker", "error", err)
	} else {
		logger.Info("Registered with tracker", "address", multiaddr)
	}
}

func (m *Manager) RefreshTrackerRegistration(ctx context.Context) {
	m.refreshRegistration(ctx)
}

func (m *Manager) UpdateTrackerURL(ctx context.Context, newURL string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if newURL == m.trackerURL {
		return
	}

	logger.Info("Updating tracker URL", "from", m.trackerURL, "to", newURL)
	m.trackerURL = newURL
	m.tracker = NewTrackerClient(newURL)

	go m.discoverFromTrackerWithRetry(ctx)
}

func (m *Manager) AddBootstrapPeer(ctx context.Context, peerAddr peer.AddrInfo) error {
	if m.dht == nil {
		return fmt.Errorf("DHT not initialized")
	}

	logger.Info("Adding bootstrap peer", "peer", peerAddr.ID)
	if err := m.host.Connect(ctx, peerAddr); err != nil {
		return fmt.Errorf("failed to connect to bootstrap peer: %w", err)
	}
	if err := m.dht.Bootstrap(ctx); err != nil {
		return fmt.Errorf("failed to bootstrap DHT: %w", err)
	}

	m.savePeer(peerAddr, false)
	return nil
}

func (m *Manager) loadPersistedPeers() error {
	peers, err := m.persistence.Load()
	if err != nil {
		return err
	}

	m.mu.Lock()
	for _, p := range peers {
		m.peers[p.ID] = &p
	}
	m.mu.Unlock()

	logger.Info("Loaded persisted peers", "count", len(peers))
	return nil
}

func (m *Manager) discoverFromTrackerWithRetry(ctx context.Context) {
	retryDelay := 1 * time.Second
	maxRetryDelay := 60 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if err := m.discoverFromTracker(ctx); err != nil {
				logger.Warn("Tracker discovery failed, retrying", "error", err, "retryDelay", retryDelay)
				time.Sleep(retryDelay)
				retryDelay *= 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
			} else {
				retryDelay = 1 * time.Second // Reset on success
				time.Sleep(5 * time.Minute)  // Check tracker periodically
			}
		}
	}
}

func (m *Manager) discoverFromTracker(ctx context.Context) error {
	if m.trackerURL == "" {
		return nil // No tracker configured
	}

	peers, err := m.tracker.FetchPeers(ctx)
	if err != nil {
		return err
	}

	logger.Info("Tracker returned peers", "count", len(peers))
	for _, p := range peers {
		if p.ID == m.host.ID() {
			continue
		}

		m.savePeer(p, true)
		if err := m.host.Connect(ctx, p); err != nil {
			logger.Warn("Failed to connect to tracker peer", "peer", p.ID, "error", err)
		} else {
			logger.Info("Connected to tracker peer", "peer", p.ID)
			m.addAsBootstrap(ctx, p)
		}
	}
	return nil
}

func (m *Manager) addAsBootstrap(ctx context.Context, p peer.AddrInfo) {
	if m.dht == nil {
		return
	}

	logger.Info("Running DHT bootstrap after connecting to tracker peer", "peer", p.ID)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := m.dht.Bootstrap(ctx); err != nil {
		logger.Warn("DHT bootstrap warning", "error", err)
	}
	logger.Info("DHT routing table size after bootstrap", "size", m.dht.RoutingTable().Size())
}

func (m *Manager) savePeer(p peer.AddrInfo, skipAutoConnect bool) {
	m.mu.Lock()
	if _, exists := m.peers[p.ID]; !exists {
		if len(m.peers) >= m.maxPeers {
			m.mu.Unlock()
			return
		}

		m.peers[p.ID] = &p
		m.mu.Unlock()

		m.persistPeers()
		logger.Info("Saved new peer", "peer", p.ID)
		if !skipAutoConnect {
			m.tryConnectToPeer(p)
		}
	} else {
		m.mu.Unlock()
	}
}

func (m *Manager) tryConnectToPeer(p peer.AddrInfo) {
	if p.ID == m.host.ID() {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		logger.Debug("Attempting to connect to discovered peer", "peer", p.ID)
		if err := m.host.Connect(ctx, p); err != nil {
			logger.Warn("Failed to connect to discovered peer", "peer", p.ID, "error", err)
		} else {
			logger.Info("Connected to discovered peer", "peer", p.ID)
			if m.dht != nil {
				if err := m.dht.Bootstrap(ctx); err != nil {
					logger.Warn("DHT bootstrap after peer connect", "error", err)
				}
			}
			if m.networkManager != nil {
				addr := ""
				if len(p.Addrs) > 0 {
					addr = p.Addrs[0].String()
				}
				m.networkManager.NotifyPeerConnected(p.ID, addr)
			}
		}
	}()
}

// GetAllPeers returns all known peers (persisted)
func (m *Manager) GetAllPeers() []peer.AddrInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var peersList []peer.AddrInfo
	for _, p := range m.peers {
		peersList = append(peersList, *p)
	}
	return peersList
}

func (m *Manager) persistPeers() {
	m.mu.RLock()
	var peersList []peer.AddrInfo
	for _, peer := range m.peers {
		peersList = append(peersList, *peer)
	}
	m.mu.RUnlock()

	if err := m.persistence.Save(peersList); err != nil {
		logger.Error("Failed to save peers to disk", "error", err)
	} else {
		logger.Info("Persisted peers to disk", "count", len(peersList))
	}
}

func (m *Manager) initDHT(ctx context.Context) error {
	connectedPeers := m.host.Network().Peers()
	var bootstrapPeers []peer.AddrInfo
	for _, pid := range connectedPeers {
		if pid == m.host.ID() {
			continue
		}

		addrs := m.host.Peerstore().Addrs(pid)
		if len(addrs) > 0 {
			bootstrapPeers = append(bootstrapPeers, peer.AddrInfo{ID: pid, Addrs: addrs})
		}
	}

	var opts []dht.Option
	opts = append(opts, dht.Mode(dht.ModeServer))
	if len(bootstrapPeers) > 0 {
		opts = append(opts, dht.BootstrapPeers(bootstrapPeers...))
		logger.Info("DHT initialized with bootstrap peers", "count", len(bootstrapPeers))
	}

	kademliaDHT, err := dht.New(ctx, m.host, opts...)
	if err != nil {
		return err
	}

	m.dht = kademliaDHT
	if err := kademliaDHT.Bootstrap(ctx); err != nil {
		logger.Warn("DHT bootstrap warning", "error", err)
	}

	m.discovery = routing.NewRoutingDiscovery(kademliaDHT)
	logger.Info("DHT initialized successfully")
	logger.Info("DHT routing table size", "count", m.dht.RoutingTable().Size())
	return nil
}

func (m *Manager) waitForPeers(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connectedPeers := len(m.host.Network().Peers())
		if connectedPeers > 1 {
			logger.Debug("Found connected peers, proceeding with DHT", "count", connectedPeers-1)
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	logger.Debug("Timeout waiting for peers", "found", len(m.host.Network().Peers())-1)
	return false
}

func (m *Manager) populateDHTFromConnectedPeers() {
	if m.dht == nil {
		return
	}

	peers := m.host.Network().Peers()
	count := 0
	for _, peerID := range peers {
		if peerID == m.host.ID() {
			continue
		}
		count++
	}
	if count > 0 {
		logger.Info("Running DHT bootstrap", "peerCount", count)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.dht.Bootstrap(ctx); err != nil {
			logger.Error("DHT bootstrap failed", "error", err)
		}
		logger.Info("DHT routing table size", "count", m.dht.RoutingTable().Size())
	}
}

func (m *Manager) advertisePeriodically(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.discovery != nil {
				m.discovery.Advertise(ctx, "raag-music")
				logger.Info("DHT re-advertised presence")
			}
		}
	}
}

func (m *Manager) discoverViaDHT(ctx context.Context) {
	if m.discovery == nil {
		return
	}

	logger.Info("DHT: Advertising presence...")
	m.discovery.Advertise(ctx, "raag-music")
	logger.Info("DHT: Advertisement complete")

	logger.Info("DHT: Waiting for peer connections...")
	hasPeers := m.waitForPeers(60 * time.Second)
	m.populateDHTFromConnectedPeers()
	if !hasPeers {
		logger.Info("DHT: No peers connected, skipping DHT discovery")
		return
	}

	logger.Info("DHT: Starting peer discovery...")
	queryCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	peerChan, err := m.discovery.FindPeers(queryCtx, "raag-music")
	if err != nil {
		logger.Error("DHT FindPeers failed", "error", err)
		return
	}

	discovered := make(map[peer.ID]bool)
	for {
		select {
		case p := <-peerChan:
			if p.ID == "" || p.ID == m.host.ID() {
				continue
			}
			if discovered[p.ID] {
				continue
			}

			discovered[p.ID] = true
			logger.Info("DHT discovered peer", "peer", p.ID)
			m.savePeer(p, false) // auto-connect in background via tryConnectToPeer
		case <-ctx.Done():
			return
		}
	}
}

func (m *Manager) discoverViaDHTRetry(ctx context.Context) {
	m.discoverViaDHT(ctx)
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logger.Info("DHT: Retrying peer discovery...")
			m.discoverViaDHT(ctx)
		}
	}
}

func (m *Manager) discoverViaMDNS(ctx context.Context) {
	notifee := &mdnsNotifee{manager: m}
	service := mdns.NewMdnsService(m.host, "raag-music", notifee)
	if err := service.Start(); err != nil {
		logger.Error("mDNS discovery failed", "error", err)
		return
	}

	select {
	case <-time.After(10 * time.Second):
		logger.Info("mDNS discovery timeout")
	case <-ctx.Done():
		return
	}
}

type mdnsNotifee struct {
	manager *Manager
}

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.manager.host.ID() {
		return
	}
	logger.Info("mDNS discovered peer", "peer", pi.ID)
	n.manager.savePeer(pi, false)
}
