package discovery

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/storage"
	"golang.org/x/sync/errgroup"
)

type Manager struct {
	host                host.Host
	ctx                 context.Context
	dht                 *dht.IpfsDHT
	discovery           *routing.RoutingDiscovery
	maxPeers            int
	listenHost          string
	rendezvous          string
	dhtEnabled          bool
	bootstrapPeers      []string
	mdnsEnabled         bool
	mdnsServiceName     string
	pubsub              *PubSubManager
	peerStore           *storage.PeerStore
	onPeerSave          func(peers []peer.AddrInfo)
	mdnsPeerCount       uint64
	mdnsService         mdns.Service
	mdnsNotifee         *mdnsNotifee
	heartbeatEvery      time.Duration
	mdnsRetryDelay      time.Duration
	pendingSongCallback func(peer.ID, LibraryAnnounceMessage)
	stateMu             sync.RWMutex
}

type ManagerConfig struct {
	Host            host.Host
	AuthSecret      string
	MaxPeers        int
	ListenHost      string
	Rendezvous      string
	DHTEnabled      bool
	BootstrapPeers  []string
	MDNSEnabled     bool
	MDNSServiceName string
	PeerStore       *storage.PeerStore
}

func NewManager(cfg ManagerConfig) *Manager {
	bootstrapPeers := cfg.BootstrapPeers
	if len(bootstrapPeers) == 0 {
		logger.Debugf("Using default IPFS bootstrap peers for DHT")
		bootstrapPeers = constants.DefaultBootstrapPeers
	}
	return &Manager{
		host:            cfg.Host,
		maxPeers:        cfg.MaxPeers,
		listenHost:      cfg.ListenHost,
		rendezvous:      cfg.Rendezvous,
		dhtEnabled:      cfg.DHTEnabled,
		bootstrapPeers:  slices.Clone(bootstrapPeers),
		mdnsEnabled:     cfg.MDNSEnabled,
		mdnsServiceName: cfg.MDNSServiceName,
		peerStore:       cfg.PeerStore,
		heartbeatEvery:  constants.TrackerHeartbeatInterval,
		mdnsRetryDelay:  constants.MDNSRetryInitialDelay,
	}
}

func (m *Manager) SetOnPeerSave(callback func(peers []peer.AddrInfo)) {
	m.onPeerSave = callback
}

func (m *Manager) SetTestIntervals(heartbeat, refresh, retryDelay, maxRetryDelay time.Duration) {
	if heartbeat > 0 {
		m.heartbeatEvery = heartbeat
	}
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
	DHTEnabled     bool       `json:"dht_enabled"`
	DHTPeers       int        `json:"dht_peers"`
	MDNSEnabled    bool       `json:"mdns_enabled"`
	MDNSDiscovered int        `json:"mdns_discovered"`
	ConnectedPeers []PeerInfo `json:"connected_peers"`
	KnownPeers     []PeerInfo `json:"known_peers"`
}

func (m *Manager) GetNetworkState() NetworkState {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()

	state := NetworkState{
		SelfID:         m.host.ID().String(),
		ListenAddr:     "",
		Mode:           "networked",
		DHTEnabled:     m.dhtEnabled,
		DHTPeers:       0,
		MDNSEnabled:    m.mdnsEnabled && m.listenHost != "127.0.0.1" && m.listenHost != "localhost",
		MDNSDiscovered: 0,
		ConnectedPeers: []PeerInfo{},
		KnownPeers:     []PeerInfo{},
	}

	addrs := m.host.Addrs()
	if len(addrs) > 0 {
		state.ListenAddr = addrs[0].String()
	}
	if m.dht != nil {
		state.DHTPeers = m.dht.RoutingTable().Size()
	}

	connectedPeers := m.host.Network().Peers()
	connectedPeersMap := make(map[peer.ID]bool, len(connectedPeers))
	for _, pid := range connectedPeers {
		connectedPeersMap[pid] = true
	}

	const maxInt32 = int64(1<<31 - 1)
	if m.mdnsPeerCount > uint64(maxInt32) {
		state.MDNSDiscovered = int(maxInt32)
	} else {
		state.MDNSDiscovered = int(m.mdnsPeerCount)
	}

	knownPeerIDs := m.host.Peerstore().Peers()
	for _, pid := range knownPeerIDs {
		if pid == m.host.ID() {
			continue
		}

		var addrStr string
		addrs := m.host.Peerstore().Addrs(pid)
		if len(addrs) > 0 {
			addrStr = addrs[0].String()
		}

		connected := connectedPeersMap[pid]
		state.KnownPeers = append(state.KnownPeers, PeerInfo{
			ID:            pid.String(),
			Addr:          addrStr,
			Connected:     connected,
			DiscoveredVia: "DHT/mDNS",
		})
	}

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

	logger.Infof("--- Discovery ---")
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
	m.ctx = ctx

	if err := m.connectBootstrapPeers(ctx); err != nil {
		logger.Warnf("Failed to connect configured bootstrap peers error=%v", err)
	}

	g, ctx := errgroup.WithContext(ctx)

	mdnsCanStart := m.mdnsEnabled && m.listenHost != "127.0.0.1" && m.listenHost != "localhost"
	if m.mdnsEnabled && !mdnsCanStart {
		if m.listenHost == "127.0.0.1" || m.listenHost == "localhost" {
			logger.Debugf("Skipping mDNS discovery (localhost mode)")
		}
	}
	if mdnsCanStart {
		logger.Infof("Starting mDNS discovery (service=%s)", m.getMDNSServiceName())
		logger.Infof("mDNS: ensure UDP port 5353 is open on firewall for local peer discovery")
		g.Go(func() error {
			m.discoverViaMDNS(ctx)
			return nil
		})
	} else if !m.mdnsEnabled {
		logger.Infof("mDNS discovery disabled via config")
	}

	if !m.dhtEnabled {
		logger.Infof("DHT discovery disabled")
		if err := g.Wait(); err != nil {
			logger.Warnf("Background service error: %v", err)
		}
		return nil
	}

	logger.Debugf("Initializing DHT...")
	if err := m.initDHT(ctx); err != nil {
		logger.Errorf("Failed to initialize DHT error=%v", err)
	}

	if err := m.initPubSub(ctx); err != nil {
		logger.Warnf("Failed to initialize PubSub: %v", err)
	} else {
		logger.Infof("GossipSub peer discovery active")
	}

	g.Go(func() error {
		m.logNetworkStatus(ctx)
		return nil
	})
	g.Go(func() error {
		m.oneShotDHTDiscovery(ctx)
		return nil
	})

	if err := g.Wait(); err != nil {
		logger.Warnf("Background service error: %v", err)
	}
	return nil
}

func isReachableAddr(addr multiaddr.Multiaddr) bool {
	ipStr, err := addr.ValueForProtocol(multiaddr.P_IP4)
	if err != nil {
		return true
	}

	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return false
	}
	return !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsMulticast()
}

func filterReachableAddresses(addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
	var filtered []multiaddr.Multiaddr
	for _, addr := range addrs {
		if isReachableAddr(addr) {
			filtered = append(filtered, addr)
		} else {
			logger.Debugf("Filtered private address: %s", addr.String())
		}
	}
	if len(filtered) == 0 {
		logger.Debugf("No public addresses available (NAT detected)")
		return nil
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

func (m *Manager) savePeer(p peer.AddrInfo, skipAutoConnect bool) {
	connectedPeers := m.host.Network().Peers()
	connectedPeersMap := make(map[peer.ID]bool, len(connectedPeers))
	for _, pid := range connectedPeers {
		connectedPeersMap[pid] = true
	}

	knownPeers := m.host.Peerstore().Peers()
	knownPeersMap := make(map[peer.ID]bool, len(knownPeers))
	for _, pid := range knownPeers {
		knownPeersMap[pid] = true
	}

	alreadyConnected := connectedPeersMap[p.ID]
	alreadyKnown := knownPeersMap[p.ID]
	if !alreadyKnown {
		if len(knownPeers) >= m.maxPeers {
			return
		}

		m.host.Peerstore().AddAddrs(p.ID, p.Addrs, peerstore.PermanentAddrTTL)
		logger.Debugf("Saved new peer peer=%s", p.ID)

		if m.peerStore != nil && len(p.Addrs) > 0 {
			if err := m.peerStore.SavePeer(m.ctx, p); err != nil {
				logger.Debugf("Failed to persist peer to Badger: %v", err)
			}
		}

		if !skipAutoConnect && !alreadyConnected {
			m.tryConnectToPeer(p)
		} else if alreadyConnected {
			logger.Debugf("Peer already connected, skipping connection attempt peer=%s", p.ID)
		}
	}
}

func (m *Manager) tryConnectToPeer(p peer.AddrInfo) {
	if p.ID == m.host.ID() {
		return
	}
	go m.connectWithRetry(p)
}

func (m *Manager) connectWithRetry(p peer.AddrInfo) {
	if len(m.host.Network().ConnsToPeer(p.ID)) > 0 {
		logger.Debugf("Already connected to peer, skipping peer=%s", p.ID)
		return
	}
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
		connectCtx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
		logger.Debugf("Attempting to connect to peer peer=%s attempt=%d/%d", p.ID, attempt+1, maxRetries)
		if err := m.host.Connect(connectCtx, p); err != nil {
			cancel()
			if attempt < maxRetries-1 {
				logger.Warnf("Connection failed, retrying in %v peer=%s error=%v", retryDelays[attempt], p.ID, err)
				select {
				case <-m.ctx.Done():
					return
				case <-time.After(retryDelays[attempt]):
				}
				continue
			}
			logger.Warnf("Failed to connect to peer after %d attempts peer=%s error=%v", maxRetries, p.ID, err)
			return
		}

		logger.Infof("Connected to discovered peer peer=%s", p.ID)
		cancel()
		return
	}
}

func (m *Manager) GetAllPeers() []peer.AddrInfo {
	peerIDs := m.host.Peerstore().Peers()
	peersList := make([]peer.AddrInfo, 0, len(peerIDs))
	for _, pid := range peerIDs {
		if pid == m.host.ID() {
			continue
		}
		addrs := m.host.Peerstore().Addrs(pid)
		if len(addrs) > 0 {
			peersList = append(peersList, peer.AddrInfo{ID: pid, Addrs: addrs})
		}
	}
	return peersList
}

func (m *Manager) connectBootstrapPeers(ctx context.Context) error {
	for _, multiaddrStr := range m.bootstrapPeers {
		addrInfo, err := peer.AddrInfoFromString(multiaddrStr)
		if err != nil {
			logger.Debugf("Ignoring invalid bootstrap peer multiaddr=%s error=%v", multiaddrStr, err)
			continue
		}
		if addrInfo.ID == m.host.ID() {
			continue
		}

		m.savePeer(*addrInfo, true)
		if err := m.host.Connect(ctx, *addrInfo); err != nil {
			logger.Debugf("Failed to connect bootstrap peer peer=%s error=%v", addrInfo.ID, err)
			continue
		}
		logger.Infof("Connected bootstrap peer peer=%s", addrInfo.ID)
	}
	return nil
}

func (m *Manager) initDHT(ctx context.Context) error {
	connectedPeers := m.host.Network().Peers()
	connectedPeerSet := make(map[peer.ID]bool)
	var bootstrapPeers []peer.AddrInfo
	for _, pid := range connectedPeers {
		if pid == m.host.ID() {
			continue
		}

		connectedPeerSet[pid] = true
		addrs := m.host.Peerstore().Addrs(pid)
		if len(addrs) > 0 {
			bootstrapPeers = append(bootstrapPeers, peer.AddrInfo{ID: pid, Addrs: addrs})
		}
	}

	allPeers := m.host.Peerstore().Peers()
	for _, pid := range allPeers {
		if pid == m.host.ID() || connectedPeerSet[pid] {
			continue
		}

		addrs := m.host.Peerstore().Addrs(pid)
		if len(addrs) > 0 {
			bootstrapPeers = append(bootstrapPeers, peer.AddrInfo{ID: pid, Addrs: addrs})
		}
	}
	if len(allPeers) > 0 {
		logger.Debugf("DHT seeded with persisted peers count=%d", len(allPeers)-1)
	}

	defaultBootstrap := dht.GetDefaultBootstrapPeerAddrInfos()
	bootstrapPeers = append(bootstrapPeers, defaultBootstrap...)
	logger.Debugf("DHT total bootstrap peers: %d", len(bootstrapPeers))

	var opts []dht.Option
	opts = append(opts, dht.Mode(dht.ModeAutoServer))
	if len(bootstrapPeers) > 0 {
		opts = append(opts, dht.BootstrapPeers(bootstrapPeers...))
		logger.Debugf("DHT initialized with bootstrap peers count=%d", len(bootstrapPeers))
	}

	kademliaDHT, err := dht.New(ctx, m.host, opts...)
	if err != nil {
		return err
	}

	m.dht = kademliaDHT
	logger.Infof("DHT initialized in mode: %v", kademliaDHT.Mode())
	if err := kademliaDHT.Bootstrap(ctx); err != nil {
		logger.Warnf("DHT bootstrap warning error=%v", err)
	}

	m.discovery = routing.NewRoutingDiscovery(kademliaDHT)
	logger.Debugf("DHT initialized successfully")
	logger.Debugf("DHT routing table size (initial) size=%d", m.dht.RoutingTable().Size())
	return nil
}

func (m *Manager) logNetworkStatus(ctx context.Context) {
	ticker := time.NewTicker(constants.MDNSLogInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dhtSize := 0
			peerstoreCount := 0
			connectedCount := 0
			mdnsPeers := 0
			if m.dht != nil {
				dhtSize = m.dht.RoutingTable().Size()
			}
			if m.host != nil {
				peerstoreCount = len(m.host.Peerstore().Peers())
				connectedCount = len(m.host.Network().Peers())
			}

			m.stateMu.RLock()
			const maxInt32 = int64(1<<31 - 1)
			if m.mdnsPeerCount > uint64(maxInt32) {
				mdnsPeers = int(maxInt32)
			} else {
				mdnsPeers = int(m.mdnsPeerCount)
			}
			m.stateMu.RUnlock()
			logger.Debugf("Network status: dht_routing=%d peerstore=%d connected=%d mdns=%d",
				dhtSize, peerstoreCount, connectedCount, mdnsPeers)
		}
	}
}

func (m *Manager) oneShotDHTDiscovery(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(constants.DHTBootstrapDelay):
	}

	if m.discovery == nil {
		return
	}

	logger.Debugf("DHT: Advertising presence...")
	_, err := m.discovery.Advertise(ctx, m.rendezvous)
	if err != nil {
		logger.Debugf("DHT advertise failed (expected if no DHT peers yet): %v", err)
	}

	logger.Debugf("DHT: Starting one-shot peer discovery...")
	queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	peerChan, err := m.discovery.FindPeers(queryCtx, m.rendezvous)
	if err != nil {
		logger.Warnf("DHT FindPeers error: %v", err)
		return
	}

	knownPeersList := m.host.Peerstore().Peers()
	knownPeersMap := make(map[peer.ID]bool, len(knownPeersList))
	for _, pid := range knownPeersList {
		knownPeersMap[pid] = true
	}

	count := 0
	for p := range peerChan {
		if p.ID == "" || p.ID == m.host.ID() {
			continue
		}
		if knownPeersMap[p.ID] {
			continue
		}
		count++
		logger.Debugf("DHT discovered peer: %s", p.ID)
		m.savePeer(p, false)
	}
	logger.Debugf("DHT discovery complete: found %d new peers", count)
}

func (m *Manager) getMDNSServiceName() string {
	if m.mdnsServiceName != "" {
		return m.mdnsServiceName
	}
	return constants.MDNSServiceNameDefault
}

func (m *Manager) discoverViaMDNS(ctx context.Context) {
	logger.Infof("mDNS: service name=%s, refresh_interval=%v", m.getMDNSServiceName(), constants.MDNSRefreshInterval)
	for {
		select {
		case <-ctx.Done():
			if m.mdnsService != nil {
				logger.Infof("mDNS: stopping service")
				if err := m.mdnsService.Close(); err != nil {
					logger.Debugf("mDNS: error closing service: %v", err)
				}
			}
			logger.Infof("mDNS: discovery stopped")
			return
		default:
		}

		err := m.startMDNSService(ctx)
		if err != nil {
			logger.Warnf("mDNS: service error, retrying in %v: %v", m.mdnsRetryDelay, err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(m.mdnsRetryDelay):
			}

			if m.mdnsRetryDelay < constants.MDNSRetryMaxDelay {
				m.mdnsRetryDelay *= 2
			}
		}
	}
}

func (m *Manager) startMDNSService(ctx context.Context) error {
	mdnsNotifee := &mdnsNotifee{manager: m}
	service := mdns.NewMdnsService(m.host, m.getMDNSServiceName(), mdnsNotifee)
	if err := service.Start(); err != nil {
		return fmt.Errorf("failed to start mDNS service: %w", err)
	}

	m.mdnsService = service
	m.mdnsNotifee = mdnsNotifee
	m.mdnsRetryDelay = constants.MDNSRetryInitialDelay

	logger.Infof("mDNS: service started successfully")
	refreshTicker := time.NewTicker(constants.MDNSRefreshInterval)
	defer refreshTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-refreshTicker.C:
			logger.Debugf("mDNS: refreshing discovery")
			if err := service.Close(); err != nil {
				logger.Debugf("mDNS: error closing service: %v", err)
			}
			return nil
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

	knownPeersList := n.manager.host.Peerstore().Peers()
	knownPeersMap := make(map[peer.ID]bool, len(knownPeersList))
	for _, pid := range knownPeersList {
		knownPeersMap[pid] = true
	}

	alreadyKnown := knownPeersMap[pi.ID]
	if !alreadyKnown {
		n.manager.stateMu.Lock()
		n.manager.mdnsPeerCount++
		n.manager.stateMu.Unlock()
		logger.Infof("mDNS: discovered new peer %s", pi.ID)
	} else {
		logger.Debugf("mDNS: peer already known, skipping peer=%s", pi.ID)
		return
	}
	n.manager.savePeer(pi, false)
}

func (m *Manager) initPubSub(ctx context.Context) error {
	if m.host == nil {
		return fmt.Errorf("host not initialized")
	}

	psm, err := NewPubSubManager(ctx, m.host)
	if err != nil {
		return err
	}

	psm.OnPeerDiscover = func(peerInfo peer.AddrInfo) {
		if peerInfo.ID == m.host.ID() {
			return
		}
		m.savePeer(peerInfo, false)
	}

	m.pubsub = psm
	if m.pendingSongCallback != nil {
		psm.OnSongAnnounce = m.pendingSongCallback
	}
	return psm.Start(ctx)
}

func (m *Manager) SetSongAnnounceCallback(cb func(peer.ID, LibraryAnnounceMessage)) {
	m.pendingSongCallback = cb
	if m.pubsub != nil {
		m.pubsub.OnSongAnnounce = cb
	}
}

func (m *Manager) PublishSongAnnounce(action string, song SongInfo) {
	if m.pubsub == nil || m.ctx == nil {
		return
	}
	m.pubsub.PublishLibraryAnnounce(m.ctx, action, song)
}

func (m *Manager) DHT() *dht.IpfsDHT {
	return m.dht
}
