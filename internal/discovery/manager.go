package discovery

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
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
	rendezvous     string
	dhtEnabled     bool
	bootstrapPeers []string
	authKeyPair    ed25519.PrivateKey
	authToken      *auth.AuthToken
	networkManager interface {
		NotifyPeerConnected(peerID peer.ID, addr string)
	}
	onPeerSave      func(peers []peer.AddrInfo)
	mdnsPeerCount   uint64
	connectingPeers map[peer.ID]bool
	connectedPeers  map[peer.ID]time.Time
	heartbeatEvery  time.Duration
	refreshEvery    time.Duration
	retryDelay      time.Duration
	maxRetryDelay   time.Duration
}

func NewManager(h host.Host, identityKeyBytes []byte, trackerURL string, maxPeers int, listenHost string, rendezvous string, dhtEnabled bool, bootstrapPeers []string) *Manager {
	var authKeyPair ed25519.PrivateKey
	var err error
	if len(identityKeyBytes) > 0 {
		authKeyPair, err = auth.DeriveAuthKey(identityKeyBytes)
		if err != nil {
			logger.Warnf("Failed to derive auth key from identity: %v", err)
		} else {
			pubKey := hex.EncodeToString(authKeyPair.Public().(ed25519.PublicKey))
			logger.Infof("Derived auth key from identity (pubkey: %s)", pubKey)
		}
	}
	if authKeyPair == nil {
		authKeyPair, err = auth.GenerateAuthKeyPair()
		if err != nil {
			logger.Warnf("Failed to generate auth key pair: %v", err)
		} else {
			logger.Warnf("No identity key provided, generated random auth key")
		}
	}
	return &Manager{
		host:            h,
		peers:           make(map[peer.ID]*peer.AddrInfo),
		persistence:     NewPeerPersistence(),
		tracker:         NewTrackerClient(trackerURL),
		maxPeers:        maxPeers,
		trackerURL:      trackerURL,
		listenHost:      listenHost,
		rendezvous:      rendezvous,
		dhtEnabled:      dhtEnabled,
		bootstrapPeers:  slices.Clone(bootstrapPeers),
		authKeyPair:     authKeyPair,
		connectingPeers: make(map[peer.ID]bool),
		connectedPeers:  make(map[peer.ID]time.Time),
		heartbeatEvery:  constants.TrackerHeartbeatInterval,
		refreshEvery:    constants.TrackerRefreshInterval,
		retryDelay:      constants.TrackerRetryInitialDelay,
		maxRetryDelay:   constants.TrackerRetryMaxDelay,
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

func (m *Manager) SetTestIntervals(heartbeat, refresh, retryDelay, maxRetryDelay time.Duration) {
	if heartbeat > 0 {
		m.heartbeatEvery = heartbeat
	}
	if refresh > 0 {
		m.refreshEvery = refresh
	}
	if retryDelay > 0 {
		m.retryDelay = retryDelay
	}
	if maxRetryDelay > 0 {
		m.maxRetryDelay = maxRetryDelay
	}
}

func (m *Manager) MarkPeerConnected(peerID peer.ID) {
	m.mu.Lock()
	m.connectedPeers[peerID] = time.Now()
	delete(m.connectingPeers, peerID)
	m.mu.Unlock()
	logger.Debugf("Marked peer as connected peer=%s", peerID)
}

type PeerInfo struct {
	ID            string    `json:"id"`
	Addr          string    `json:"addr"`
	Connected     bool      `json:"connected"`
	DiscoveredVia string    `json:"discovered_via"`
	FirstSeen     time.Time `json:"first_seen"`
	LastSeen      time.Time `json:"last_seen"`
}

type NetworkState struct {
	SelfID         string     `json:"self_id"`
	ListenAddr     string     `json:"listen_addr"`
	Mode           string     `json:"mode"`
	TrackerURL     string     `json:"tracker_url"`
	TrackerStatus  string     `json:"tracker_status"`
	DHTEnabled     bool       `json:"dht_enabled"`
	DHTPeers       int        `json:"dht_peers"`
	MDNSEnabled    bool       `json:"mdns_enabled"`
	MDNSDiscovered int        `json:"mdns_discovered"`
	ConnectedPeers []PeerInfo `json:"connected_peers"`
	KnownPeers     []PeerInfo `json:"known_peers"`
	AuthPublicKey  string     `json:"auth_public_key,omitempty"`
}

func (m *Manager) GetNetworkState() NetworkState {
	state := NetworkState{
		SelfID:         m.host.ID().String(),
		ListenAddr:     "",
		Mode:           "networked",
		TrackerURL:     m.trackerURL,
		TrackerStatus:  "unknown",
		DHTEnabled:     m.dhtEnabled,
		DHTPeers:       0,
		MDNSEnabled:    m.listenHost != "127.0.0.1" && m.listenHost != "localhost",
		MDNSDiscovered: 0,
		ConnectedPeers: []PeerInfo{},
		KnownPeers:     []PeerInfo{},
		AuthPublicKey:  m.GetAuthPublicKey(),
	}

	addrs := m.host.Addrs()
	if len(addrs) > 0 {
		state.ListenAddr = addrs[0].String()
	}
	if m.dht != nil {
		state.DHTPeers = m.dht.RoutingTable().Size()
	}
	connectedPeers := m.host.Network().Peers()

	m.mu.RLock()
	state.MDNSDiscovered = int(m.mdnsPeerCount)
	for pid, p := range m.peers {
		if pid == m.host.ID() {
			continue
		}

		var addrStr string
		if len(p.Addrs) > 0 {
			addrStr = p.Addrs[0].String()
		}

		connected := slices.Contains(connectedPeers, pid)
		state.KnownPeers = append(state.KnownPeers, PeerInfo{
			ID:            pid.String(),
			Addr:          addrStr,
			Connected:     connected,
			DiscoveredVia: "DHT/mDNS",
		})
	}
	m.mu.RUnlock()

	for _, pid := range connectedPeers {
		if pid == m.host.ID() {
			continue
		}

		addrs := m.host.Peerstore().Addrs(pid)
		var addrStr string
		if len(addrs) > 0 {
			addrStr = addrs[0].String()
		}
		state.ConnectedPeers = append(state.ConnectedPeers, PeerInfo{
			ID:        pid.String(),
			Addr:      addrStr,
			Connected: true,
		})
	}

	return state
}

func (m *Manager) LogNetworkState() {
	state := m.GetNetworkState()

	logger.Infof("=== P2P Network State ===")
	logger.Infof("Self: %s @ %s", state.SelfID, state.ListenAddr)
	logger.Infof("Mode: %s", state.Mode)
	if state.AuthPublicKey != "" {
		logger.Infof("Auth Key: %s...", state.AuthPublicKey[:32])
	}

	logger.Infof("--- Discovery ---")
	logger.Infof("Tracker: %s", state.TrackerURL)
	logger.Infof("DHT: enabled=%v (peers=%d)", state.DHTEnabled, state.DHTPeers)
	logger.Infof("mDNS: enabled=%v (discovered=%d)", state.MDNSEnabled, state.MDNSDiscovered)

	logger.Infof("--- Connections (%d) ---", len(state.ConnectedPeers))
	if len(state.ConnectedPeers) == 0 {
		logger.Infof("  (no active connections)")
	}
	for _, peer := range state.ConnectedPeers {
		logger.Infof("  ✓ %s @ %s", peer.ID[:12], peer.Addr)
	}

	logger.Infof("--- Known Peers (%d) ---", len(state.KnownPeers))
	if len(state.KnownPeers) == 0 {
		logger.Infof("  (no known peers)")
	}
	for _, peer := range state.KnownPeers {
		status := "○"
		if peer.Connected {
			status = "✓"
		}
		logger.Infof("  %s %s @ %s", status, peer.ID[:12], peer.Addr)
	}
}

func (m *Manager) Start(ctx context.Context) error {
	if err := m.loadPersistedPeers(); err != nil {
		logger.Errorf("Failed to load persisted peers error=%v", err)
	}

	logger.Debugf("Starting peer discovery from tracker...")
	if err := m.discoverFromTracker(ctx); err != nil {
		logger.Warnf("Initial tracker discovery failed error=%v", err)
	}

	go m.runTrackerRegistrationLoop(ctx)
	go m.runTrackerRefreshLoop(ctx)
	if err := m.connectBootstrapPeers(ctx); err != nil {
		logger.Warnf("Failed to connect configured bootstrap peers error=%v", err)
	}
	if m.listenHost == "127.0.0.1" || m.listenHost == "localhost" {
		logger.Debugf("Skipping mDNS discovery (localhost mode)")
	} else {
		logger.Debugf("Starting mDNS discovery in background...")
		go m.discoverViaMDNS(ctx)
		go m.logMDNSStatus(ctx)
	}

	if !m.dhtEnabled {
		logger.Infof("DHT discovery disabled")
		return nil
	}

	logger.Debugf("Initializing DHT with existing peer connections...")
	if err := m.initDHT(ctx); err != nil {
		logger.Errorf("Failed to initialize DHT error=%v", err)
	}
	m.populateDHTFromConnectedPeers()

	go m.logRoutingTableSize(ctx)
	go m.discoverViaDHTRetry(ctx)
	go m.advertisePeriodically(ctx)
	return nil
}

func (m *Manager) refreshRegistration(ctx context.Context) {
	if err := m.tryRefreshRegistration(ctx); err != nil {
		logger.Errorf("Failed to register with tracker error=%v", err)
	}
}

func (m *Manager) selectAdvertisedAddresses(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
	filtered := filterReachableAddresses(addrs)
	prioritized := prioritizeAddresses(filtered)
	if len(prioritized) == 0 {
		return nil
	}

	const maxAdvertisedAddrs = 6
	selected := make([]multiaddr.Multiaddr, 0, min(len(prioritized), maxAdvertisedAddrs))
	seen := make(map[string]struct{})
	for _, addr := range prioritized {
		addrStr := addr.String()
		if _, ok := seen[addrStr]; ok {
			continue
		}
		seen[addrStr] = struct{}{}
		selected = append(selected, addr)
		if len(selected) >= maxAdvertisedAddrs {
			break
		}
	}
	if len(selected) == 0 && len(addrs) > 0 {
		return addrs[:1]
	}
	return selected
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
	var quic, tcp, other []multiaddr.Multiaddr
	for _, addr := range addrs {
		addrStr := strings.ToLower(addr.String())
		if strings.Contains(addrStr, "/quic-v1") {
			quic = append(quic, addr)
		} else if strings.Contains(addrStr, "/tcp/") {
			tcp = append(tcp, addr)
		} else {
			other = append(other, addr)
		}
	}

	result := append(append(quic, tcp...), other...)
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

func (m *Manager) GetAuthPublicKey() string {
	if m.authKeyPair == nil {
		return ""
	}
	return hex.EncodeToString(m.authKeyPair.Public().(ed25519.PublicKey))
}

func (m *Manager) RefreshTrackerRegistration(ctx context.Context) {
	m.refreshRegistration(ctx)
}

func (m *Manager) runTrackerRegistrationLoop(ctx context.Context) {
	if m.trackerURL == "" {
		return
	}
	if err := m.refreshRegistrationWithRetry(ctx); err != nil {
		logger.Warnf("Initial tracker registration failed error=%v", err)
	}
}

func (m *Manager) runTrackerRefreshLoop(ctx context.Context) {
	if m.trackerURL == "" {
		return
	}

	ticker := time.NewTicker(m.refreshEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.discoverFromTrackerWithRetry(ctx); err != nil {
				logger.Warnf("Tracker peer refresh failed error=%v", err)
			}
		}
	}
}

func (m *Manager) refreshRegistrationWithRetry(ctx context.Context) error {
	if m.trackerURL == "" {
		return nil
	}

	retryDelay := m.retryDelay
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		registerCtx, cancel := context.WithTimeout(ctx, constants.HTTPClientTimeout)
		err := m.tryRefreshRegistration(registerCtx)
		cancel()
		if err == nil {
			return nil
		}

		logger.Warnf("Tracker registration failed, retrying error=%v retryDelay=%v", err, retryDelay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay):
		}

		retryDelay *= 2
		if retryDelay > m.maxRetryDelay {
			retryDelay = m.maxRetryDelay
		}
	}
}

func (m *Manager) tryRefreshRegistration(ctx context.Context) error {
	if m.trackerURL == "" {
		return nil
	}

	addrs := m.host.Addrs()
	if len(addrs) == 0 {
		return fmt.Errorf("host has no listening addresses")
	}

	advertisedAddrs := m.selectAdvertisedAddresses(addrs)
	if len(advertisedAddrs) == 0 {
		return fmt.Errorf("host has no advertisable addresses")
	}
	addrWithPeerID := make([]string, 0, len(advertisedAddrs))
	for _, addr := range advertisedAddrs {
		addrWithPeerID = append(addrWithPeerID, addr.Encapsulate(multiaddr.StringCast("/p2p/"+m.host.ID().String())).String())
	}
	peerID := m.host.ID().String()
	authData, err := m.getAuthData()
	if err != nil {
		logger.Warnf("Failed to generate auth data: %v", err)
		authData = ""
	}
	if err := m.tracker.RegisterPeer(ctx, addrWithPeerID, peerID, authData); err != nil {
		return err
	}
	logger.Infof("Registered with tracker peer_id=%s addrs=%d", peerID, len(addrWithPeerID))
	return nil
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

	logger.Debugf("Loaded persisted peers count=%d", len(peers))
	return nil
}

func (m *Manager) discoverFromTrackerWithRetry(ctx context.Context) error {
	retryDelay := 1 * time.Second
	maxRetryDelay := m.maxRetryDelay
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := m.discoverFromTracker(ctx); err != nil {
				logger.Warnf("Tracker discovery failed, retrying error=%v retryDelay=%v", err, retryDelay)
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(retryDelay):
				}
				retryDelay = min(retryDelay*2, maxRetryDelay)
			} else {
				return nil
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

	logger.Debugf("Tracker returned peers count=%d", len(peers))
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

	logger.Debugf("Running DHT bootstrap after connecting to tracker peer peer=%s", p.ID)
	bootstrapCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := m.dht.Bootstrap(bootstrapCtx); err != nil {
		logger.Warnf("DHT bootstrap warning error=%v", err)
	}
	logger.Debugf("DHT routing table size after bootstrap size=%d", m.dht.RoutingTable().Size())
}

func (m *Manager) savePeer(p peer.AddrInfo, skipAutoConnect bool) {
	m.mu.Lock()

	_, alreadyConnected := m.connectedPeers[p.ID]
	_, alreadyConnecting := m.connectingPeers[p.ID]
	if _, exists := m.peers[p.ID]; !exists {
		if len(m.peers) >= m.maxPeers {
			m.mu.Unlock()
			return
		}

		m.peers[p.ID] = &p
		m.mu.Unlock()

		m.persistPeers()
		logger.Debugf("Saved new peer peer=%s", p.ID)
		if !skipAutoConnect && !alreadyConnected && !alreadyConnecting {
			m.tryConnectToPeer(p)
		} else if alreadyConnecting {
			logger.Debugf("Peer already connecting, skipping duplicate connection attempt peer=%s", p.ID)
		} else if alreadyConnected {
			logger.Debugf("Peer already connected, skipping connection attempt peer=%s", p.ID)
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
	m.mu.Lock()
	if m.connectingPeers[p.ID] {
		m.mu.Unlock()
		logger.Debugf("Already connecting to peer, skipping peer=%s", p.ID)
		return
	}
	m.connectingPeers[p.ID] = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.connectingPeers, p.ID)
		m.mu.Unlock()
	}()

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
		connectCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		logger.Debugf("Attempting to connect to peer peer=%s attempt=%d/%d", p.ID, attempt+1, maxRetries)
		if err := m.host.Connect(connectCtx, p); err != nil {
			cancel()
			if attempt < maxRetries-1 {
				logger.Warnf("Connection failed, retrying in %v peer=%s error=%v", retryDelays[attempt], p.ID, err)
				time.Sleep(retryDelays[attempt])
				continue
			}
			logger.Warnf("Failed to connect to peer after %d attempts peer=%s error=%v", maxRetries, p.ID, err)
			return
		}

		logger.Infof("Connected to discovered peer peer=%s", p.ID)
		m.mu.Lock()
		m.connectedPeers[p.ID] = time.Now()
		m.mu.Unlock()

		if m.dht != nil {
			dhtCtx, dhtCancel := context.WithTimeout(connectCtx, 10*time.Second)
			if err := m.dht.Bootstrap(dhtCtx); err != nil {
				logger.Warnf("DHT bootstrap after peer connect error=%v", err)
			}
			dhtCancel()
		}

		cancel()
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
		logger.Debugf("Persisted peers to disk count=%d", len(peersList))
	}

	if m.onPeerSave != nil {
		m.onPeerSave(peersList)
	}
}

func (m *Manager) connectBootstrapPeers(ctx context.Context) error {
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
			continue
		}
		logger.Infof("Connected configured bootstrap peer peer=%s", addrInfo.ID)
	}
	return nil
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
		logger.Debugf("DHT initialized with bootstrap peers count=%d", len(bootstrapPeers))
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
	logger.Debugf("DHT initialized successfully")
	logger.Debugf("DHT routing table size (initial) size=%d", m.dht.RoutingTable().Size())
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
		logger.Debugf("Running DHT bootstrap peerCount=%d", count)
		bootstrapCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.dht.Bootstrap(bootstrapCtx); err != nil {
			logger.Errorf("DHT bootstrap failed error=%v", err)
		}
		logger.Debugf("DHT routing table size count=%d", m.dht.RoutingTable().Size())
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
				logger.Debugf("DHT status: routing_table_size=%d total_discovered_peers=%d active_connections=%d",
					dhtSize, connectedCount, networkPeers)
			}
		}
	}
}

func (m *Manager) discoverViaDHT(ctx context.Context) {
	if m.discovery == nil {
		return
	}

	logger.Debugf("DHT: Advertising presence...")
	m.discovery.Advertise(ctx, m.rendezvous)
	logger.Debugf("DHT: Advertisement complete")

	logger.Debugf("DHT: Waiting for peer connections...")
	hasPeers := m.waitForPeers(constants.DHTWaitForPeersTimeout)
	m.populateDHTFromConnectedPeers()
	if !hasPeers {
		logger.Debugf("DHT: No peers connected yet, starting discovery anyway")
	}

	logger.Debugf("DHT: Starting peer discovery...")
	for {
		queryCtx, cancel := context.WithTimeout(ctx, constants.DHTQueryTimeout)
		peerChan, err := m.discovery.FindPeers(queryCtx, m.rendezvous)
		if err != nil {
			cancel()
			logger.Warnf("DHT FindPeers error error=%v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(constants.DHTRetryDelay):
			}
			continue
		}

		discovered := make(map[peer.ID]bool)
		count := 0
		roundDone := false
		for {
			select {
			case <-ctx.Done():
				cancel()
				return
			case p, ok := <-peerChan:
				if !ok {
					logger.Debugf("DHT: Finished discovery round, discovered %d new peers", count)
					roundDone = true
					break
				}
				if p.ID == "" || p.ID == m.host.ID() {
					continue
				}

				m.mu.RLock()
				_, alreadyKnown := m.peers[p.ID]
				m.mu.RUnlock()
				if alreadyKnown {
					continue
				}
				if discovered[p.ID] {
					continue
				}

				discovered[p.ID] = true
				count++
				logger.Debugf("DHT discovered peer peer=%s", p.ID)
				m.savePeer(p, false)
			}
			if roundDone {
				break
			}
		}
		cancel()

		select {
		case <-ctx.Done():
			return
		case <-time.After(constants.DHTDiscoveryInterval):
		}
	}
}

func (m *Manager) discoverViaDHTRetry(ctx context.Context) {
	m.discoverViaDHT(ctx)
}

func (m *Manager) discoverViaMDNS(ctx context.Context) {
	notifee := &mdnsNotifee{manager: m}
	service := mdns.NewMdnsService(m.host, m.rendezvous, notifee)
	if err := service.Start(); err != nil {
		logger.Errorf("mDNS discovery failed error=%v", err)
		return
	}

	logger.Debugf("mDNS service started, running continuously...")
	<-ctx.Done()
	logger.Debugf("mDNS discovery stopped")
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
			logger.Debugf("mDNS status: discovered_peers=%d", mdnsCount)
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
	_, alreadyKnown := n.manager.peers[pi.ID]
	if !alreadyKnown {
		n.manager.mdnsPeerCount++
	}
	n.manager.mu.Unlock()

	if alreadyKnown {
		logger.Debugf("mDNS found already known peer, skipping peer=%s", pi.ID)
		return
	}

	logger.Debugf("mDNS discovered peer peer=%s", pi.ID)
	n.manager.savePeer(pi, false)
}
