package discovery

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/auth"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
)

type Manager struct {
	host             host.Host
	dht              *dht.IpfsDHT
	discovery        *routing.RoutingDiscovery
	peers            map[peer.ID]*peer.AddrInfo
	mu               sync.RWMutex
	persistence      *PeerPersistence
	tracker          *TrackerClient
	maxPeers         int
	trackerURL       string
	trackerRelayAddr string
	listenHost       string
	rendezvous       string
	dhtEnabled       bool
	bootstrapPeers   []string
	authKeyPair      ed25519.PrivateKey
	authToken        *auth.AuthToken
	networkManager   interface {
		NotifyPeerConnected(peerID peer.ID, addr string)
	}
	onPeerSave    func(peers []peer.AddrInfo)
	mdnsPeerCount uint64
}

func NewManager(h host.Host, trackerURL string, maxPeers int, listenHost string, rendezvous string, dhtEnabled bool, bootstrapPeers []string) *Manager {
	authKeyPair, err := auth.GenerateAuthKeyPair()
	if err != nil {
		logger.Warnf("Failed to generate auth key pair: %v", err)
	}

	return &Manager{
		host:           h,
		peers:          make(map[peer.ID]*peer.AddrInfo),
		persistence:    NewPeerPersistence(),
		tracker:        NewTrackerClient(trackerURL),
		maxPeers:       maxPeers,
		trackerURL:     trackerURL,
		listenHost:     listenHost,
		rendezvous:     rendezvous,
		dhtEnabled:     dhtEnabled,
		bootstrapPeers: slices.Clone(bootstrapPeers),
		authKeyPair:    authKeyPair,
	}
}

func (m *Manager) SetNetworkManager(nm interface {
	NotifyPeerConnected(peerID peer.ID, addr string)
},
) {
	m.networkManager = nm
}

func (m *Manager) SetOnPeerSave(callback func(peers []peer.AddrInfo)) {
	m.onPeerSave = callback
}

func (m *Manager) Start(ctx context.Context) error {
	if err := m.loadPersistedPeers(); err != nil {
		logger.Errorf("Failed to load persisted peers error=%v", err)
	}
	if m.trackerURL != "" {
		logger.Infof("Fetching tracker relay address...")
		if err := m.fetchTrackerAddr(ctx); err != nil {
			logger.Warnf("Failed to fetch tracker address: %v", err)
		}
	}

	logger.Infof("Starting peer discovery from tracker...")
	m.discoverFromTracker(ctx)
	m.registerSelfWithTracker(ctx)
	if err := m.connectBootstrapPeers(ctx); err != nil {
		logger.Warnf("Failed to connect configured bootstrap peers error=%v", err)
	}
	if m.listenHost == "127.0.0.1" || m.listenHost == "localhost" {
		logger.Infof("Skipping mDNS discovery (localhost mode)")
	} else {
		logger.Infof("Starting mDNS discovery in background...")
		go m.discoverViaMDNS(ctx)
		go m.logMDNSStatus(ctx)
	}

	if !m.dhtEnabled {
		logger.Infof("DHT discovery disabled")
		return nil
	}

	logger.Infof("Initializing DHT with existing peer connections...")
	if err := m.initDHT(ctx); err != nil {
		logger.Errorf("Failed to initialize DHT error=%v", err)
	}
	m.populateDHTFromConnectedPeers()

	go m.logRoutingTableSize(ctx)
	go m.discoverViaDHTRetry(ctx)
	go m.advertisePeriodically(ctx)
	return nil
}

func (m *Manager) fetchTrackerAddr(ctx context.Context) error {
	libp2pAddr, relayAddr, err := m.tracker.FetchTrackerAddr(ctx)
	if err != nil {
		return err
	}
	if libp2pAddr != "" {
		logger.Infof("Tracker libp2p address: %s", libp2pAddr)
		if err := m.addTrackerAsBootstrap(ctx, libp2pAddr); err != nil {
			logger.Warnf("Failed to connect to tracker: %v", err)
		}
	}
	if relayAddr != "" {
		m.trackerRelayAddr = relayAddr
		logger.Infof("Tracker relay address: %s", relayAddr)
	}
	return nil
}

func (m *Manager) addTrackerAsBootstrap(ctx context.Context, addr string) error {
	addrInfo, err := peer.AddrInfoFromString(addr)
	if err != nil {
		return fmt.Errorf("invalid tracker address: %w", err)
	}
	if err := m.host.Connect(ctx, *addrInfo); err != nil {
		return fmt.Errorf("failed to connect to tracker: %w", err)
	}

	logger.Infof("Connected to tracker for DHT bootstrap")
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

	usableAddr := m.selectUsableAddress(addrs)
	multiaddr := usableAddr.Encapsulate(multiaddr.StringCast("/p2p/" + m.host.ID().String())).String()
	authData, err := m.getAuthData()
	if err != nil {
		logger.Warnf("Failed to generate auth data: %v", err)
		authData = ""
	}

	if err := m.tracker.RegisterPeer(ctx, multiaddr, authData); err != nil {
		logger.Errorf("Failed to register with tracker error=%v", err)
	} else {
		logger.Infof("Registered with tracker address=%s", multiaddr)
	}
}

func (m *Manager) selectUsableAddress(addrs []multiaddr.Multiaddr) multiaddr.Multiaddr {
	for _, addr := range addrs {
		addrStr := addr.String()
		if isUsableAddr(addrStr) {
			logger.Debugf("Selected usable address for registration: %s", addrStr)
			return addr
		}
	}

	logger.Debugf("No usable LAN address found, using first address: %s", addrs[0].String())
	return addrs[0]
}

func isUsableAddr(addr string) bool {
	return !strings.Contains(addr, "/127.0.0.1/") &&
		!strings.Contains(addr, "/0.0.0.0/") &&
		!strings.Contains(addr, "/localhost/") &&
		strings.Contains(addr, "/ip4/")
}

func isReachableAddr(addr string) bool {
	addrLower := strings.ToLower(addr)
	if strings.Contains(addrLower, "/127.0.0.1/") || strings.Contains(addrLower, "/localhost/") {
		return false
	}
	if strings.HasPrefix(addrLower, "/ip4/172.") {
		octets := strings.SplitSeq(addrLower, "/")
		for octet := range octets {
			if len(octet) == 3 && octet[0] == '1' && octet[1] == '7' && octet[2] == '2' {
				return false
			}
		}
	}
	if strings.Contains(addrLower, "/172.1") || strings.Contains(addrLower, "/172.2") || strings.Contains(addrLower, "/172.3") {
		return false
	}
	if strings.Contains(addrLower, "/192.168.122.") || strings.Contains(addrLower, "/192.168.124.") {
		return false
	}
	if strings.Contains(addrLower, "/10.") && !strings.Contains(addrLower, "/192.168.") {
		return false
	}
	if strings.HasPrefix(addrLower, "/ip4/169.254.") {
		return false
	}
	if strings.HasPrefix(addrLower, "/ip4/224.") || strings.HasPrefix(addrLower, "/ip4/225.") || strings.HasPrefix(addrLower, "/ip4/226.") {
		return false
	}
	return true
}

func filterReachableAddresses(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
	var filtered []multiaddr.Multiaddr
	for _, addr := range addrs {
		if isReachableAddr(addr.String()) {
			filtered = append(filtered, addr)
		} else {
			logger.Debugf("Filtered unreachable address: %s", addr.String())
		}
	}
	if len(filtered) == 0 && len(addrs) > 0 {
		logger.Debugf("All addresses filtered, using original list")
		return addrs
	}
	return filtered
}

func prioritizeAddresses(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
	var lan, wan, relay, other []multiaddr.Multiaddr
	for _, addr := range addrs {
		addrStr := strings.ToLower(addr.String())
		if strings.Contains(addrStr, "/192.168.") || strings.Contains(addrStr, "/192.168.0.") {
			lan = append(lan, addr)
		} else if strings.Contains(addrStr, "/p2p-circuit") {
			relay = append(relay, addr)
		} else if strings.Contains(addrStr, "/ip4/") {
			wan = append(wan, addr)
		} else {
			other = append(other, addr)
		}
	}

	result := append(append(lan, wan...), other...)
	result = append(result, relay...)
	return result
}

func (m *Manager) getAuthData() (string, error) {
	if m.authToken == nil && m.authKeyPair != nil {
		token, err := auth.GenerateToken(m.host.ID(), m.authKeyPair)
		if err != nil {
			return "", fmt.Errorf("failed to generate auth token: %w", err)
		}
		m.authToken = token
	}
	if m.authToken == nil {
		return "", fmt.Errorf("no auth key pair available")
	}
	return auth.SerializeToken(m.authToken)
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

	logger.Infof("Updating tracker URL from=%s to=%s", m.trackerURL, newURL)
	m.trackerURL = newURL
	m.tracker = NewTrackerClient(newURL)

	go m.discoverFromTrackerWithRetry(ctx)
}

func (m *Manager) AddBootstrapPeer(ctx context.Context, peerAddr peer.AddrInfo) error {
	if !m.dhtEnabled {
		return fmt.Errorf("DHT discovery is disabled")
	}
	if m.dht == nil {
		return fmt.Errorf("DHT not initialized")
	}

	logger.Infof("Adding bootstrap peer peer=%s", peerAddr.ID)
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
		logger.Warnf("Could not load persisted peers error=%v", err)
		return nil
	}

	m.mu.Lock()
	for _, p := range peers {
		m.peers[p.ID] = &p
	}
	m.mu.Unlock()

	logger.Infof("Loaded persisted peers count=%d", len(peers))
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
				logger.Warnf("Tracker discovery failed, retrying error=%v retryDelay=%v", err, retryDelay)
				time.Sleep(retryDelay)
				retryDelay = min(retryDelay*2, maxRetryDelay)
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

	logger.Infof("Tracker returned peers count=%d", len(peers))
	for _, p := range peers {
		if p.ID == m.host.ID() {
			continue
		}

		m.savePeer(p, true)
		if err := m.host.Connect(ctx, p); err != nil {
			logger.Warnf("Failed to connect to tracker peer peer=%s error=%v", p.ID, err)
		} else {
			logger.Infof("Connected to tracker peer peer=%s", p.ID)
			m.addAsBootstrap(ctx, p)
		}
	}
	return nil
}

func (m *Manager) addAsBootstrap(ctx context.Context, p peer.AddrInfo) {
	if m.dht == nil {
		return
	}

	logger.Infof("Running DHT bootstrap after connecting to tracker peer peer=%s", p.ID)
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := m.dht.Bootstrap(ctx); err != nil {
		logger.Warnf("DHT bootstrap warning error=%v", err)
	}
	logger.Infof("DHT routing table size after bootstrap size=%d", m.dht.RoutingTable().Size())
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
		logger.Infof("Saved new peer peer=%s", p.ID)
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
	go m.connectWithRetry(p)
}

func (m *Manager) connectWithRetry(p peer.AddrInfo) {
	if len(p.Addrs) > 0 {
		filteredAddrs := filterReachableAddresses(p.Addrs)
		prioritizedAddrs := prioritizeAddresses(filteredAddrs)
		p.Addrs = prioritizedAddrs
		logger.Debugf("Filtered peer addresses from %d to %d peer=%s",
			len(p.Addrs), len(prioritizedAddrs), p.ID)
	}

	maxRetries := 3
	retryDelays := []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}
	for attempt := range maxRetries {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		logger.Debugf("Attempting to connect to peer peer=%s attempt=%d/%d", p.ID, attempt+1, maxRetries)
		if err := m.host.Connect(ctx, p); err != nil {
			if attempt < maxRetries-1 {
				logger.Warnf("Connection failed, retrying in %v peer=%s error=%v", retryDelays[attempt], p.ID, err)
				time.Sleep(retryDelays[attempt])
				continue
			}
			logger.Warnf("Failed to connect to peer after %d attempts peer=%s error=%v", maxRetries, p.ID, err)
			return
		}

		logger.Infof("Connected to discovered peer peer=%s", p.ID)
		if m.dht != nil {
			dhtCtx, dhtCancel := context.WithTimeout(ctx, 10*time.Second)
			defer dhtCancel()
			if err := m.dht.Bootstrap(dhtCtx); err != nil {
				logger.Warnf("DHT bootstrap after peer connect error=%v", err)
			}
		}
		if m.networkManager != nil {
			addr := ""
			if len(p.Addrs) > 0 {
				addr = p.Addrs[0].String()
			}
			m.networkManager.NotifyPeerConnected(p.ID, addr)
		}
		return
	}
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
		logger.Errorf("Failed to save peers to disk error=%v", err)
	} else {
		logger.Infof("Persisted peers to disk count=%d", len(peersList))
	}

	if m.onPeerSave != nil {
		m.onPeerSave(peersList)
	}
}

func (m *Manager) connectBootstrapPeers(ctx context.Context) error {
	var connectErr error
	if m.trackerURL != "" {
		logger.Infof("Fetching tracker address for DHT bootstrap...")
		if err := m.fetchTrackerAddr(ctx); err != nil {
			logger.Warnf("Failed to fetch tracker address: %v", err)
		}
	}
	for _, multiaddrStr := range m.bootstrapPeers {
		addrInfo, err := peer.AddrInfoFromString(multiaddrStr)
		if err != nil {
			logger.Warnf("Ignoring invalid bootstrap peer multiaddr=%s error=%v", multiaddrStr, err)
			continue
		}
		if addrInfo.ID == m.host.ID() {
			continue
		}

		m.savePeer(*addrInfo, true)
		if err := m.host.Connect(ctx, *addrInfo); err != nil {
			logger.Warnf("Failed to connect bootstrap peer peer=%s error=%v", addrInfo.ID, err)
			connectErr = err
			continue
		}
		logger.Infof("Connected configured bootstrap peer peer=%s", addrInfo.ID)
	}
	return connectErr
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
		logger.Infof("DHT initialized with bootstrap peers count=%d", len(bootstrapPeers))
	}

	kademliaDHT, err := dht.New(ctx, m.host, opts...)
	if err != nil {
		return err
	}

	m.dht = kademliaDHT
	if err := kademliaDHT.Bootstrap(ctx); err != nil {
		logger.Warnf("DHT bootstrap warning error=%v", err)
	}

	m.discovery = routing.NewRoutingDiscovery(kademliaDHT)
	logger.Infof("DHT initialized successfully")
	logger.Infof("DHT routing table size (initial) size=%d", m.dht.RoutingTable().Size())
	logger.Infof("Note: DHT routing table populates asynchronously as peers are discovered")
	return nil
}

func (m *Manager) waitForPeers(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		connectedPeers := len(m.host.Network().Peers())
		if connectedPeers > 1 {
			logger.Debugf("Found connected peers, proceeding with DHT count=%d", connectedPeers-1)
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	logger.Debugf("Timeout waiting for peers found=%d", len(m.host.Network().Peers())-1)
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
		logger.Infof("Running DHT bootstrap peerCount=%d", count)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.dht.Bootstrap(ctx); err != nil {
			logger.Errorf("DHT bootstrap failed error=%v", err)
		}
		logger.Infof("DHT routing table size count=%d", m.dht.RoutingTable().Size())
	}
}

func (m *Manager) advertisePeriodically(ctx context.Context) {
	ticker := time.NewTicker(constants.DHTAdvertisementInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.discovery != nil {
				m.discovery.Advertise(ctx, m.rendezvous)
				logger.Infof("DHT re-advertised presence")
			}
		}
	}
}

func (m *Manager) logRoutingTableSize(ctx context.Context) {
	ticker := time.NewTicker(constants.DHTLogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if m.dht != nil {
				m.mu.RLock()
				connectedCount := len(m.peers)
				m.mu.RUnlock()

				dhtSize := m.dht.RoutingTable().Size()
				networkPeers := len(m.host.Network().Peers())
				logger.Infof("DHT status: routing_table_size=%d connected_peers=%d network_peers=%d",
					dhtSize, connectedCount, networkPeers)
			}
		}
	}
}

func (m *Manager) discoverViaDHT(ctx context.Context) {
	if m.discovery == nil {
		return
	}

	logger.Infof("DHT: Advertising presence...")
	m.discovery.Advertise(ctx, m.rendezvous)
	logger.Infof("DHT: Advertisement complete")

	logger.Infof("DHT: Waiting for peer connections...")
	hasPeers := m.waitForPeers(constants.DHTWaitForPeersTimeout)
	m.populateDHTFromConnectedPeers()

	if !hasPeers {
		logger.Infof("DHT: No peers connected yet, starting discovery anyway")
	}

	logger.Infof("DHT: Starting peer discovery...")
	for {
		queryCtx, cancel := context.WithTimeout(ctx, constants.DHTQueryTimeout)
		defer cancel()

		peerChan, err := m.discovery.FindPeers(queryCtx, m.rendezvous)
		if err != nil {
			logger.Warnf("DHT FindPeers error error=%v", err)
			time.Sleep(constants.DHTRetryDelay)
			continue
		}

		discovered := make(map[peer.ID]bool)
		count := 0
		for {
			select {
			case <-ctx.Done():
				return
			case p, ok := <-peerChan:
				if !ok {
					logger.Infof("DHT: Finished discovery round, discovered %d new peers", count)
					goto nextRound
				}
				if p.ID == "" || p.ID == m.host.ID() {
					continue
				}
				if discovered[p.ID] {
					continue
				}

				discovered[p.ID] = true
				count++
				logger.Infof("DHT discovered peer peer=%s", p.ID)
				m.savePeer(p, false)
			}
		}
	nextRound:
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}

func (m *Manager) discoverViaDHTRetry(ctx context.Context) {
	m.discoverViaDHT(ctx)
	ticker := time.NewTicker(constants.DHTDiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logger.Infof("DHT: Running periodic discovery...")
			m.discoverViaDHT(ctx)
		}
	}
}

func (m *Manager) discoverViaMDNS(ctx context.Context) {
	notifee := &mdnsNotifee{manager: m}
	service := mdns.NewMdnsService(m.host, m.rendezvous, notifee)
	if err := service.Start(); err != nil {
		logger.Errorf("mDNS discovery failed error=%v", err)
		return
	}

	logger.Infof("mDNS service started, running continuously...")
	<-ctx.Done()
	logger.Infof("mDNS discovery stopped")
}

func (m *Manager) logMDNSStatus(ctx context.Context) {
	ticker := time.NewTicker(constants.MDNSLogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.RLock()
			mdnsCount := m.mdnsPeerCount
			m.mu.RUnlock()
			logger.Infof("mDNS status: discovered_peers=%d", mdnsCount)
		}
	}
}

type mdnsNotifee struct {
	manager *Manager
}

func (n *mdnsNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.manager.host.ID() {
		return
	}

	n.manager.mu.Lock()
	n.manager.mdnsPeerCount++
	n.manager.mu.Unlock()
	logger.Infof("mDNS discovered peer peer=%s", pi.ID)
	n.manager.savePeer(pi, false)
}
