package network

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoreds"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/auth"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/discovery"
	"github.com/p-society/raag/internal/library"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/metadata"
	"github.com/p-society/raag/internal/storage"
	"github.com/p-society/raag/internal/transfer"
	"github.com/spf13/viper"
)

func generateSessionNonce() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		nonce := time.Now().UnixNano()
		return fmt.Sprintf("%d", nonce)
	}
	return hex.EncodeToString(bytes)
}

type serializedKey struct {
	Type string `json:"type"`
	Data []byte `json:"data"`
}

func loadOrGenerateIdentity() (crypto.PrivKey, error) {
	keyPath, err := config.IdentityKeyPath()
	if err != nil {
		logger.Warnf("Could not get identity key path, generating new key: %v", err)
		key, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
		if err != nil {
			return nil, err
		}
		return key, nil
	}

	dir, err := config.Dir()
	if err != nil {
		logger.Warnf("Could not get config dir, generating new key: %v", err)
		key, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
		if err != nil {
			return nil, err
		}
		return key, nil
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		if os.IsNotExist(err) {
			key, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
			if err != nil {
				return nil, err
			}
			return key, nil
		}

		logger.Warnf("Could not open config root, generating new key: %v", err)
		key, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
		if err != nil {
			return nil, err
		}
		return key, nil
	}

	relPath, err := filepath.Rel(dir, keyPath)
	if err != nil {
		logger.Warnf("Could not compute relative path, generating new key: %v", err)
		key, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
		if err != nil {
			return nil, err
		}
		return key, nil
	}

	data, err := root.ReadFile(relPath)
	if err == nil {
		var sk serializedKey
		if err := json.Unmarshal(data, &sk); err != nil {
			logger.Warnf("Could not parse identity key, generating new one: %v", err)
			return generateAndSaveKey(keyPath)
		}

		switch sk.Type {
		case "RSA", "Ed25519":
		default:
			logger.Warnf("Unknown key type %s, generating new key", sk.Type)
			return generateAndSaveKey(keyPath)
		}

		key, err := crypto.UnmarshalPrivateKey(sk.Data)
		if err != nil {
			logger.Warnf("Could not unmarshal identity key, generating new one: %v", err)
			return generateAndSaveKey(keyPath)
		}

		logger.Debugf("Loaded existing identity key from %s", keyPath)
		return key, nil
	}
	if !os.IsNotExist(err) {
		logger.Warnf("Error reading identity key: %v", err)
	}
	return generateAndSaveKey(keyPath)
}

func generateAndSaveKey(keyPath string) (crypto.PrivKey, error) {
	key, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	keyData, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key: %w", err)
	}

	sk := serializedKey{
		Type: "RSA",
		Data: keyData,
	}

	data, err := json.Marshal(sk)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key data: %w", err)
	}

	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.WriteFile(keyPath, data, 0o600); err != nil {
		logger.Warnf("Failed to save identity key: %v", err)
	} else {
		logger.Debugf("Generated and saved new identity key to %s", keyPath)
	}
	return key, nil
}

type idleTimeoutWriter struct {
	stream  network.Stream
	timeout time.Duration
}

func (w *idleTimeoutWriter) Write(p []byte) (int, error) {
	_ = w.stream.SetWriteDeadline(time.Now().Add(w.timeout))
	return w.stream.Write(p)
}

type idleTimeoutReader struct {
	stream  network.Stream
	timeout time.Duration
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	_ = r.stream.SetReadDeadline(time.Now().Add(r.timeout))
	return r.stream.Read(p)
}

type NetworkManager struct {
	host              host.Host
	ctx               context.Context
	cfg               *config.Config
	viper             *viper.Viper
	library           *library.Library
	musicDir          string
	Online            bool
	OnPeerJoin        func(peer.ID)
	OnPeerLeave       func(peer.ID)
	OnStateChange     func(bool)
	discovery         *discovery.Manager
	pingService       *ping.PingService
	authEnabled       bool
	authorizedPeers   map[string]struct{}
	authorizedMu      sync.RWMutex
	usedNonces        map[string]time.Time
	nonceMu           sync.Mutex
	connectedPeers    map[peer.ID]bool
	peersMu           sync.RWMutex
	peerConnectCh     chan struct{}
	pendingHandshakes map[peer.ID]chan struct{}
	handshakeMu       sync.Mutex
	connectionGater   *RaagConnectionGater
	transferMgr       *transfer.Manager
}

func (n *NetworkManager) getContext() context.Context {
	if n.ctx != nil {
		return n.ctx
	}
	return context.Background()
}

func (n *NetworkManager) Host() host.Host {
	return n.host
}

func (n *NetworkManager) Close() error {
	if n.host == nil {
		return nil
	}
	return n.host.Close()
}

func (n *NetworkManager) SetAuthEnabled(enabled bool) {
	n.authorizedMu.Lock()
	defer n.authorizedMu.Unlock()
	n.authEnabled = enabled
	if enabled {
		n.authorizedPeers = make(map[string]struct{})
		logger.Infof("P2P authentication enabled")
	} else {
		n.authorizedPeers = nil
		logger.Infof("P2P authentication disabled")
	}
}

func (n *NetworkManager) AuthorizePeer(peerID peer.ID) {
	n.authorizedMu.Lock()
	defer n.authorizedMu.Unlock()
	if n.authorizedPeers != nil {
		n.authorizedPeers[peerID.String()] = struct{}{}
		logger.Debugf("Peer authorized: %s", peerID)
	}
	if n.connectionGater != nil {
		n.connectionGater.AllowPeer(peerID)
	}
}

func (n *NetworkManager) IsPeerAuthorized(peerID peer.ID) bool {
	n.authorizedMu.RLock()
	defer n.authorizedMu.RUnlock()
	if !n.authEnabled {
		return true
	}
	if n.authorizedPeers == nil {
		return false
	}
	_, authorized := n.authorizedPeers[peerID.String()]
	return authorized
}

func (n *NetworkManager) GetAuthData() (string, error) {
	if n.discovery != nil {
		return n.discovery.GetAuthData()
	}
	return "", fmt.Errorf("discovery manager not available")
}

func (n *NetworkManager) IsAuthEnabled() bool {
	n.authorizedMu.RLock()
	defer n.authorizedMu.RUnlock()
	return n.authEnabled
}

func (n *NetworkManager) VerifyAuthToken(tokenData string, expectedPeerID string, sessionNonce string) error {
	if tokenData == "" {
		if n.IsAuthEnabled() {
			return fmt.Errorf("authentication required but no token provided")
		}
		return nil
	}

	token, err := auth.DeserializeToken(tokenData)
	if err != nil {
		return fmt.Errorf("invalid token format: %w", err)
	}

	if expectedPeerID != "" && token.PeerID != expectedPeerID {
		return fmt.Errorf("peer ID mismatch: token is for %s, expected %s", token.PeerID, expectedPeerID)
	}

	if err := n.verifyNonceUsed(sessionNonce); err != nil {
		return err
	}

	valid, err := auth.VerifyToken(token)
	if err != nil {
		return fmt.Errorf("token verification failed: %w", err)
	}
	if !valid {
		return fmt.Errorf("token verification failed")
	}

	return nil
}

func (n *NetworkManager) verifyNonceUsed(nonce string) error {
	if nonce == "" {
		return nil
	}

	n.nonceMu.Lock()
	defer n.nonceMu.Unlock()

	if n.usedNonces == nil {
		n.usedNonces = make(map[string]time.Time)
	}

	if _, exists := n.usedNonces[nonce]; exists {
		return fmt.Errorf("replay attack detected: nonce already used")
	}

	n.usedNonces[nonce] = time.Now()

	go func() {
		time.Sleep(5 * time.Minute)
		n.nonceMu.Lock()
		delete(n.usedNonces, nonce)
		n.nonceMu.Unlock()
	}()

	return nil
}

func NewNetwork(cfg *config.Config, v *viper.Viper, lib *library.Library, musicDir string, store *storage.Store) (*NetworkManager, error) {
	return newNetworkWithIdentity(cfg, v, lib, musicDir, store, nil)
}

func newNetworkWithIdentity(cfg *config.Config, v *viper.Viper, lib *library.Library, musicDir string, store *storage.Store, identity crypto.PrivKey) (*NetworkManager, error) {
	logger.Debugf("Network config network=%v host=%s port=%d rendezvous=%s", cfg.Network, cfg.Host, cfg.Port, cfg.Rendezvous)

	prvKey := identity
	var err error
	if prvKey == nil {
		if store != nil {
			identityStore := storage.NewIdentityStore(store, "")
			prvKey, err = identityStore.LoadOrGenerate(context.Background())
			if err != nil {
				return nil, fmt.Errorf("failed to load identity: %w", err)
			}
		} else {
			prvKey, err = loadOrGenerateIdentity()
			if err != nil {
				return nil, fmt.Errorf("failed to load identity: %w", err)
			}
		}
	}

	var opts []libp2p.Option

	var ps peerstore.Peerstore
	if store != nil {
		ps, err = pstoreds.NewPeerstore(context.Background(), store.Peers, pstoreds.DefaultOpts())
		if err != nil {
			return nil, fmt.Errorf("failed to create persistent peerstore: %w", err)
		}
		opts = append(opts, libp2p.Peerstore(ps))
		logger.Debugf("Using persistent peerstore backed by Badger")
	}

	tcpListenAddr := fmt.Sprintf("/ip4/%s/tcp/%d", cfg.Host, cfg.Port)
	quicListenAddr := fmt.Sprintf("/ip4/%s/udp/%d/quic-v1", cfg.Host, cfg.Port)
	wsListenAddr := fmt.Sprintf("/ip4/%s/tcp/%d/ws", cfg.Host, cfg.Port+1)
	tcpMultiAddr, _ := multiaddr.NewMultiaddr(tcpListenAddr)
	quicMultiAddr, _ := multiaddr.NewMultiaddr(quicListenAddr)
	wsMultiAddr, _ := multiaddr.NewMultiaddr(wsListenAddr)
	opts = append(opts, libp2p.ListenAddrs(tcpMultiAddr, quicMultiAddr, wsMultiAddr), libp2p.Identity(prvKey))
	if cfg.Network {
		logger.Debugf("Using networked mode with NAT traversal")
		opts = append(opts, libp2p.DefaultTransports)
		if resourceManager, err := newResourceManager(); err != nil {
			logger.Warnf("Failed to initialize resource manager error=%v", err)
		} else {
			opts = append(opts, libp2p.ResourceManager(resourceManager))
		}

		cm, err := newConnectionManager()
		if err != nil {
			logger.Warnf("Failed to initialize connection manager error=%v", err)
		} else {
			opts = append(opts, libp2p.ConnectionManager(cm))
			logger.Debugf("Connection manager initialized with limits")
		}

		opts = append(opts, libp2p.EnableRelay())
		if cfg.ForceRelay {
			logger.Infof("Force relay mode enabled - skipping hole punching, using relay for all connections")
		} else {
			opts = append(opts, libp2p.EnableHolePunching())
			opts = append(opts, libp2p.NATPortMap())
			opts = append(opts, libp2p.EnableNATService())
			logger.Debugf("NAT traversal enabled: circuit relay, hole punching, UPnP, AutoNAT")
		}

		relayAddrs := cfg.RelayAddress
		if relayAddrs == "" && cfg.ForceRelay {
			logger.Infof("Using default public relays for force-relay mode")
		}
		if relayAddrs != "" {
			ma, err := multiaddr.NewMultiaddr(relayAddrs)
			if err != nil {
				logger.Warnf("Failed to parse relay address: %v", err)
			} else {
				info, err := peer.AddrInfoFromP2pAddr(ma)
				if err != nil {
					logger.Warnf("Failed to parse relay address: %v", err)
				} else {
					opts = append(opts, libp2p.EnableAutoRelayWithStaticRelays([]peer.AddrInfo{*info}))
					logger.Infof("Auto-relay enabled with custom relay: %s", relayAddrs)
				}
			}
		} else if cfg.ForceRelay {
			var relayInfos []peer.AddrInfo
			for _, addr := range constants.DefaultRelayAddrs {
				ma, err := multiaddr.NewMultiaddr(addr)
				if err != nil {
					continue
				}

				info, err := peer.AddrInfoFromP2pAddr(ma)
				if err != nil {
					continue
				}
				relayInfos = append(relayInfos, *info)
			}
			if len(relayInfos) > 0 {
				opts = append(opts, libp2p.EnableAutoRelayWithStaticRelays(relayInfos))
				logger.Infof("Auto-relay enabled with %d default public relays", len(relayInfos))
			}
		}
	} else {
		logger.Debugf("Using offline mode with limited transports")
		opts = append(opts, libp2p.DefaultTransports)
	}

	connectionGater := NewRaagConnectionGater(nil)
	opts = append(opts, libp2p.ConnectionGater(connectionGater))
	host, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	logger.Debugf("Connection gater initialized")
	pingService := ping.NewPingService(host)
	maxPeers := cfg.MaxPeers
	if maxPeers == 0 {
		maxPeers = constants.DefaultMaxPeers
	}

	discoveryMgr := discovery.NewManager(discovery.ManagerConfig{
		Host:            host,
		AuthSecret:      cfg.AuthSecret,
		TrackerURL:      cfg.TrackerURL,
		MaxPeers:        maxPeers,
		ListenHost:      cfg.Host,
		Rendezvous:      cfg.Rendezvous,
		DHTEnabled:      cfg.DHTEnabled,
		BootstrapPeers:  cfg.BootstrapPeers,
		MDNSEnabled:     cfg.MDNSEnabled,
		MDNSServiceName: cfg.MDNSServiceName,
	})
	nm := &NetworkManager{
		host:            host,
		cfg:             cfg,
		viper:           v,
		library:         lib,
		musicDir:        musicDir,
		discovery:       discoveryMgr,
		pingService:     pingService,
		peerConnectCh:   make(chan struct{}, 1),
		connectionGater: connectionGater,
		transferMgr:     transfer.NewManager(host, nil, musicDir),
	}

	discoveryMgr.SetNetworkManager(nm)
	host.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(n network.Network, conn network.Conn) {
			nm.handlePeerConnect(conn.RemotePeer(), conn.RemoteMultiaddr())
		},
		DisconnectedF: func(n network.Network, conn network.Conn) {
			nm.handlePeerDisconnect(conn.RemotePeer(), conn.RemoteMultiaddr())
		},
	})
	return nm, nil
}

func newResourceManager() (network.ResourceManager, error) {
	scalingLimits := rcmgr.DefaultLimits
	libp2p.SetDefaultServiceLimits(&scalingLimits)
	scalingLimits.AddProtocolLimit(
		protocol.ID(constants.ShareProtocolID),
		rcmgr.BaseLimit{
			Streams:         constants.TransferMaxConcurrentStreams,
			StreamsInbound:  constants.TransferMaxInboundStreams,
			StreamsOutbound: constants.TransferMaxOutboundStreams,
			Memory:          constants.TransferMemoryLimit,
			FD:              0,
		},
		rcmgr.BaseLimitIncrease{
			Streams:         0,
			StreamsInbound:  0,
			StreamsOutbound: 0,
			Memory:          0,
		},
	)
	scalingLimits.AddProtocolLimit(
		protocol.ID(constants.BlockProtocolID),
		rcmgr.BaseLimit{
			Streams:         constants.TransferMaxConcurrentStreams,
			StreamsInbound:  constants.TransferMaxInboundStreams,
			StreamsOutbound: constants.TransferMaxOutboundStreams,
			Memory:          constants.TransferMemoryLimit,
			FD:              0,
		},
		rcmgr.BaseLimitIncrease{
			Streams:         0,
			StreamsInbound:  0,
			StreamsOutbound: 0,
			Memory:          0,
		},
	)

	limitConfig := scalingLimits.AutoScale()
	rm, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limitConfig))
	if err != nil {
		return nil, err
	}

	logger.Infof("Resource manager initialized: transfer_streams=%d/%d inbound/outbound memory=%dMB",
		constants.TransferMaxConcurrentStreams,
		constants.TransferMaxInboundStreams,
		constants.TransferMemoryLimit/(1024*1024))

	return rm, nil
}

func newConnectionManager() (*connmgr.BasicConnMgr, error) {
	lowWater := 50
	highWater := 100
	gracePeriod := 30 * time.Second
	cm, err := connmgr.NewConnManager(lowWater, highWater, connmgr.WithGracePeriod(gracePeriod))
	if err != nil {
		return nil, fmt.Errorf("failed to create connection manager: %w", err)
	}

	logger.Debugf("Connection manager initialized: low=%d high=%d grace=%v", lowWater, highWater, gracePeriod)
	return cm, nil
}

func (n *NetworkManager) Start(ctx context.Context) error {
	n.ctx = ctx
	n.connectedPeers = make(map[peer.ID]bool)
	n.host.SetStreamHandler(protocol.ID(constants.ShareProtocolID), n.handleStream)
	n.host.SetStreamHandler(protocol.ID(constants.HandshakeProtocolID), n.handleHandshake)
	n.host.SetStreamHandler(protocol.ID(constants.BlockProtocolID), n.handleBlockStream)
	if err := n.discovery.Start(ctx); err != nil {
		logger.Errorf("Discovery failed to start error=%v", err)
	}

	// Wire DHT routing into the transfer manager once DHT is initialised.
	// DHT init is async inside discovery.Start, so we poll briefly.
	go func() {
		for range 20 {
			if dht := n.discovery.DHT(); dht != nil {
				n.transferMgr.SetRouting(dht)
				logger.Debugf("TransferManager: DHT routing wired")
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
		}
		logger.Warnf("TransferManager: DHT not available after 10s, block DHT lookups disabled")
	}()

	go n.healthCheckLoop(ctx)

	addrs := n.host.Addrs()
	if len(addrs) > 0 {
		logger.Debugf("Your Raag Node Multiaddress address=%s", fmt.Sprintf("%s/p2p/%s", addrs[0], n.host.ID()))
		if len(addrs) > 1 {
			logger.Debugf("Additional addresses")
			for _, addr := range addrs[1:] {
				logger.Debugf("Additional address address=%s", fmt.Sprintf("%s/p2p/%s", addr, n.host.ID()))
			}
		}
		if n.cfg.Network {
			logger.Debugf("TIP: If peers cannot connect, ensure firewall allows incoming connections on port %d (TCP)", n.cfg.Port)
		}
	} else {
		logger.Warnf("No listening addresses found host=%s port=%d", n.cfg.Host, n.cfg.Port)
	}

	<-ctx.Done()
	return ctx.Err()
}

func (n *NetworkManager) healthCheckLoop(ctx context.Context) {
	healthTicker := time.NewTicker(constants.PeerHealthCheckInterval)
	metricsTicker := time.NewTicker(constants.PeerMetricsLogInterval)
	defer healthTicker.Stop()
	defer metricsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-healthTicker.C:
			n.runHealthCheck(ctx)
		case <-metricsTicker.C:
			n.logMetrics()
		}
	}
}

func (n *NetworkManager) logMetrics() {
	connectedCount := len(n.host.Network().Peers())
	if connectedCount > 0 {
		logger.Infof("Network status: %d peers connected", connectedCount)
	}
}

func (n *NetworkManager) runHealthCheck(ctx context.Context) {
	if n.pingService == nil {
		return
	}

	n.peersMu.RLock()
	peers := make([]peer.ID, 0, len(n.connectedPeers))
	for p := range n.connectedPeers {
		peers = append(peers, p)
	}
	n.peersMu.RUnlock()

	for _, p := range peers {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ctx, cancel := context.WithTimeout(ctx, constants.PeerPingTimeout)
		resultChan := n.pingService.Ping(ctx, p)
		cancel()

		select {
		case <-ctx.Done():
			return
		case result := <-resultChan:
			if result.Error != nil {
				logger.Warnf("Health check failed for peer=%s error=%v", p, result.Error)
				if n.host.Network().Connectedness(p) != network.Connected {
					logger.Warnf("Peer no longer reachable, removing peer=%s", p)
					n.peersMu.Lock()
					delete(n.connectedPeers, p)
					n.peersMu.Unlock()
				}
			}
		}
	}
}

// GetPeers returns information about currently connected peers
func (n *NetworkManager) GetPeers() []peer.AddrInfo {
	peerIDs := n.host.Network().Peers()
	peers := make([]peer.AddrInfo, 0, len(peerIDs))
	for _, peerID := range peerIDs {
		if conns := n.host.Network().ConnsToPeer(peerID); len(conns) > 0 {
			peers = append(peers, peer.AddrInfo{
				ID:    peerID,
				Addrs: []multiaddr.Multiaddr{conns[0].RemoteMultiaddr()},
			})
		}
	}
	return peers
}

// IsOnline returns true if there are any known peers
func (n *NetworkManager) IsOnline() bool {
	return len(n.host.Network().Peers()) > 0
}

// GetPeerCount returns the number of known peers
func (n *NetworkManager) GetPeerCount() int {
	return len(n.host.Network().Peers())
}

func (n *NetworkManager) WaitForPeers(timeout time.Duration) bool {
	if n.IsOnline() {
		return true
	}
	select {
	case <-n.peerConnectCh:
		return n.IsOnline()
	case <-time.After(timeout):
		return n.IsOnline()
	}
}

func (n *NetworkManager) Connect(ctx context.Context, addrInfo peer.AddrInfo) error {
	if err := n.host.Connect(ctx, addrInfo); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	logger.Debugf("Connected to peer peer_id=%s", addrInfo.ID)
	if n.discovery != nil {
		n.discovery.RefreshTrackerRegistration(ctx)
	}
	return nil
}

func (n *NetworkManager) Disconnect(peerID peer.ID) error {
	if err := n.host.Network().ClosePeer(peerID); err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	logger.Infof("Disconnected from peer peer_id=%s", peerID)
	return nil
}

func (n *NetworkManager) GetPeerID() peer.ID {
	return n.host.ID()
}

func (n *NetworkManager) GetMultiaddr() string {
	addrs := n.host.Addrs()
	if len(addrs) == 0 {
		return fmt.Sprintf("/p2p/%s", n.host.ID())
	}
	for _, addr := range addrs {
		if isUsableAddr(addr.String()) {
			return fmt.Sprintf("%s/p2p/%s", addr, n.host.ID())
		}
	}

	logger.Debugf("No usable LAN address found, using first address: %s", addrs[0].String())
	return fmt.Sprintf("%s/p2p/%s", addrs[0], n.host.ID())
}

func isUsableAddr(addr string) bool {
	ma, err := multiaddr.NewMultiaddr(addr)
	if err != nil {
		return false
	}

	protocols := ma.Protocols()
	for _, p := range protocols {
		if p.Code == multiaddr.P_IP4 || p.Code == multiaddr.P_IP6 {
			comp, err := ma.ValueForProtocol(p.Code)
			if err != nil {
				continue
			}

			ip, err := netip.ParseAddr(comp)
			if err != nil {
				continue
			}
			if ip.IsLoopback() || ip.IsUnspecified() {
				return false
			}
			return true
		}
	}
	return false
}

func (n *NetworkManager) GetAllKnownPeers() []peer.AddrInfo {
	if n.discovery == nil {
		return nil
	}
	return n.discovery.GetAllPeers()
}

func (n *NetworkManager) LogNetworkState() {
	if n.discovery != nil {
		n.discovery.LogNetworkState()
	}
}

func (n *NetworkManager) GetNetworkState() (discovery.NetworkState, error) {
	if n.discovery == nil {
		return discovery.NetworkState{}, fmt.Errorf("discovery not initialized")
	}
	return n.discovery.GetNetworkState(), nil
}

func (n *NetworkManager) GetAuthPublicKey() string {
	if n.discovery == nil {
		return ""
	}
	return n.discovery.GetAuthPublicKey()
}

// BroadcastSongAdded announces a new song to the network via GossipSub
func (n *NetworkManager) BroadcastSongAdded(song metadata.Song) {
	if n.discovery == nil {
		return
	}
	info := discovery.SongInfo{
		Title:  song.Title,
		Artist: song.Artist,
		Album:  song.Album,
		Hash:   song.Hash,
		Size:   song.Size,
	}
	n.discovery.PublishSongAnnounce("add", info)
}

// BroadcastSongRemoved announces a song removal to the network via GossipSub
func (n *NetworkManager) BroadcastSongRemoved(song metadata.Song) {
	if n.discovery == nil {
		return
	}
	info := discovery.SongInfo{
		Title:  song.Title,
		Artist: song.Artist,
		Album:  song.Album,
		Hash:   song.Hash,
		Size:   song.Size,
	}
	n.discovery.PublishSongAnnounce("remove", info)
}

// SetSongAnnounceHandler sets the callback for receiving song announcements from peers
func (n *NetworkManager) SetSongAnnounceHandler(handler func(peer.ID, discovery.LibraryAnnounceMessage)) {
	if n.discovery != nil {
		n.discovery.SetSongAnnounceCallback(handler)
	}
}

// ShareSong pushes a song to a specific peer using the legacy
// /raag/share/1.0.0 stream protocol (length-prefixed JSON metadata + raw
// bytes).  It is the push counterpart to RequestSong.
//
// Use this when you have a direct peer reference and want to send a file
// immediately.  Use ProvideSong + RequestSong (the Phase-4 pull model) when
// the remote peer should fetch on demand via DHT provider records.
//
// Both mechanisms coexist: incoming files received via handleStream are
// automatically announced to the DHT via ProvideSong so they become
// available to other peers via the pull model as well.
func (n *NetworkManager) ShareSong(peerInfo *peer.AddrInfo, song metadata.Song) error {
	logger.Debugf("ShareSong function called peer_info=%v song=%v", peerInfo, song)
	if song.Size > constants.TransferMaxFileSize {
		return fmt.Errorf("file exceeds max transfer size: %d", song.Size)
	}

	cleanPath := filepath.Clean(song.Path)
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return fmt.Errorf("failed to resolve file path: %w", err)
	}

	root, err := os.OpenRoot(n.musicDir)
	if err != nil {
		return fmt.Errorf("error opening music root: %w", err)
	}

	relPath, err := filepath.Rel(n.musicDir, absPath)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	file, err := root.Open(relPath)
	if err != nil {
		return fmt.Errorf("open file for transfer: %w", err)
	}
	defer file.Close()

	ctx, cancel := context.WithTimeout(n.getContext(), 10*time.Second)
	defer cancel()
	stream, err := n.host.NewStream(ctx, peerInfo.ID, protocol.ID(constants.ShareProtocolID))
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}
	defer stream.Close()

	authToken, _ := n.GetAuthData()
	sessionNonce := generateSessionNonce()
	meta := buildTransferMetadata(song, song.Size, song.Hash, authToken, sessionNonce)
	if err := writeTransferMetadata(stream, meta); err != nil {
		_ = stream.Reset()
		return fmt.Errorf("failed to send transfer metadata: %w", err)
	}

	writer := &idleTimeoutWriter{stream: stream, timeout: constants.TransferIdleTimeout}
	if _, err := io.Copy(writer, file); err != nil {
		_ = stream.Reset()
		return fmt.Errorf("failed to send song data: %w", err)
	}

	logger.Debugf("ShareSong function completed successfully")
	return nil
}

func (n *NetworkManager) handleStream(stream network.Stream) {
	peerID := stream.Conn().RemotePeer()
	logger.Debugf("handleStream called peer_id=%s", peerID)
	meta, err := readTransferMetadata(stream)
	if err != nil {
		_ = stream.Reset()
		logger.Errorf("Error reading metadata from peer peer_id=%s error=%v", peerID, err)
		return
	}
	defer stream.Close()

	if err := n.VerifyAuthToken(meta.AuthToken, peerID.String(), meta.SessionNonce); err != nil {
		_ = stream.Reset()
		logger.Warnf("Auth verification failed for peer peer_id=%s error=%v", peerID, err)
		return
	}
	if meta.SizeBytes == 0 {
		logger.Debugf("Received empty file transfer from peer peer_id=%s", peerID)
		return
	}
	if meta.SizeBytes > constants.TransferMaxFileSize {
		_ = stream.Reset()
		logger.Errorf("Transfer rejected: file too large peer_id=%s size=%d", peerID, meta.SizeBytes)
		return
	}

	pID := peerID.String()
	safeTitle := sanitizeTransferName(meta.Title)
	ext := strings.ToLower(meta.Extension)
	if ext == "" {
		ext = ".bin"
	}

	fileName := fmt.Sprintf("%s_%s%s", pID, safeTitle, ext)
	saveDir := n.musicDir
	if saveDir == "" {
		saveDir = "."
	}
	if err := os.MkdirAll(saveDir, 0o750); err != nil {
		logger.Errorf("Error creating directory error=%v", err)
		return
	}

	filePath := filepath.Join(saveDir, fileName)
	tmpFile, err := os.CreateTemp(saveDir, fileName+".*.part")
	if err != nil {
		_ = stream.Reset()
		logger.Errorf("Error creating temp file error=%v", err)
		return
	}

	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		if _, statErr := os.Stat(tmpPath); statErr == nil {
			_ = os.Remove(tmpPath)
		}
	}()

	logger.Debugf("Preparing to save file file_path=%s", filePath)
	hasher := sha256.New()
	writer := io.MultiWriter(tmpFile, hasher)
	reader := &idleTimeoutReader{stream: stream, timeout: constants.TransferIdleTimeout}
	bytesWritten, err := io.CopyN(writer, reader, meta.SizeBytes)
	if err != nil {
		_ = stream.Reset()
		logger.Errorf("Error saving song error=%v", err)
		return
	}
	if bytesWritten != meta.SizeBytes {
		_ = stream.Reset()
		logger.Errorf("Incomplete transfer: wrote=%d expected=%d", bytesWritten, meta.SizeBytes)
		return
	}
	if meta.SHA256 != "" {
		digest := fmt.Sprintf("%x", hasher.Sum(nil))
		if digest != meta.SHA256 {
			_ = stream.Reset()
			logger.Errorf("Hash mismatch for received song peer_id=%s expected=%s got=%s", peerID, meta.SHA256, digest)
			return
		}
	}
	if err := tmpFile.Sync(); err != nil {
		_ = stream.Reset()
		logger.Errorf("Error syncing temp file error=%v", err)
		return
	}
	if err := tmpFile.Close(); err != nil {
		_ = stream.Reset()
		logger.Errorf("Error closing temp file error=%v", err)
		return
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = stream.Reset()
		logger.Errorf("Error finalizing received song error=%v", err)
		return
	}
	if n.library != nil {
		if err := n.library.ScanMusicLibrary(n.musicDir); err != nil {
			logger.Warnf("Failed to rescan library after receiving song error=%v", err)
		}
	}

	// Auto-announce the received song to the DHT so it is available for the
	// chunked pull model (/raag/blocks/1.0.0).  We extract metadata
	// from the saved file rather than relying on the wire metadata to ensure
	// the Song struct (hash, size, path) is fully populated.
	if song, err := metadata.ExtractMetadata(filePath); err == nil {
		go n.ProvideSong(song)
	} else {
		logger.Warnf("handleStream: could not extract metadata for DHT announce file_path=%s error=%v", filePath, err)
	}

	logger.Debugf("Song data saved bytes_written=%d", bytesWritten)
	logger.Infof("Successfully received and saved song title=%s peer_id=%s file_path=%s", meta.Title, peerID, filePath)
}

func (n *NetworkManager) handlePeerConnect(peerID peer.ID, addr multiaddr.Multiaddr) {
	if peerID == n.host.ID() {
		return
	}
	if n.IsAuthEnabled() {
		go n.initiateHandshake(peerID)
	}
	n.notifyPeerConnected(peerID, addr.String())
}

// NotifyPeerConnected is called when a peer connection is established externally
func (n *NetworkManager) NotifyPeerConnected(peerID peer.ID, addr string) {
	n.notifyPeerConnected(peerID, addr)
}

func (n *NetworkManager) notifyPeerConnected(peerID peer.ID, _ string) {
	if peerID == n.host.ID() {
		return
	}
	if n.IsAuthEnabled() {
		done := n.waitForHandshake(peerID)
		if !done {
			logger.Warnf("Handshake failed - rejecting peer peer_id=%s", peerID)
			return
		}
	}

	n.peersMu.Lock()
	_, alreadyConnected := n.connectedPeers[peerID]
	n.connectedPeers[peerID] = true
	peerCount := len(n.connectedPeers)
	n.peersMu.Unlock()

	conns := n.host.Network().ConnsToPeer(peerID)
	if len(conns) > 0 {
		if !alreadyConnected {
			addr := conns[0].RemoteMultiaddr().String()
			logger.Infof("Peer connected peer_id=%s addr=%s total_peers=%d", peerID, addr, peerCount)
			select {
			case n.peerConnectCh <- struct{}{}:
			default:
			}
		}
		if n.OnPeerJoin != nil {
			n.OnPeerJoin(peerID)
		}
		if !n.Online {
			n.Online = true
			logger.Infof("Network: Online - %d peers connected", peerCount)
			if n.OnStateChange != nil {
				n.OnStateChange(true)
			}
		}
	}
}

func (n *NetworkManager) waitForHandshake(peerID peer.ID) bool {
	n.handshakeMu.Lock()
	if n.pendingHandshakes == nil {
		n.pendingHandshakes = make(map[peer.ID]chan struct{})
	}

	doneCh, exists := n.pendingHandshakes[peerID]
	if exists {
		n.handshakeMu.Unlock()
		<-doneCh
		n.handshakeMu.Lock()
		delete(n.pendingHandshakes, peerID)
		n.handshakeMu.Unlock()
		return true
	}

	doneCh = make(chan struct{})
	n.pendingHandshakes[peerID] = doneCh
	n.handshakeMu.Unlock()

	select {
	case <-doneCh:
	case <-time.After(constants.HandshakeTimeout):
	}

	n.handshakeMu.Lock()
	delete(n.pendingHandshakes, peerID)
	close(doneCh)
	n.handshakeMu.Unlock()

	_, stillConnected := n.connectedPeers[peerID]
	return stillConnected
}

func (n *NetworkManager) handlePeerDisconnect(peerID peer.ID, _ multiaddr.Multiaddr) {
	conns := n.host.Network().ConnsToPeer(peerID)
	n.peersMu.Lock()
	if n.connectedPeers[peerID] {
		delete(n.connectedPeers, peerID)
		peerCount := len(n.connectedPeers)
		n.peersMu.Unlock()
		logger.Infof("Peer disconnected peer_id=%s remaining_peers=%d", peerID, peerCount)
	} else {
		n.peersMu.Unlock()
	}

	if len(conns) == 0 {
		if n.OnPeerLeave != nil {
			n.OnPeerLeave(peerID)
		}

		peerCount := len(n.host.Network().Peers())
		if peerCount == 0 && n.Online {
			n.Online = false
			logger.Infof("Network: Offline - no peers connected")
			if n.OnStateChange != nil {
				n.OnStateChange(false)
			}
		}
	}
}

// UpdateTrackerURL updates the tracker URL for discovery
func (n *NetworkManager) UpdateTrackerURL(ctx context.Context, newURL string) {
	if n.discovery != nil {
		n.discovery.UpdateTrackerURL(ctx, newURL)
	}
}

// AddBootstrapPeer adds a bootstrap peer to the discovery system
func (n *NetworkManager) AddBootstrapPeer(ctx context.Context, multiaddrStr string) error {
	if n.discovery == nil {
		return fmt.Errorf("discovery not initialized")
	}

	peerAddr, err := peer.AddrInfoFromString(multiaddrStr)
	if err != nil {
		return fmt.Errorf("invalid multiaddress: %w", err)
	}
	return n.discovery.AddBootstrapPeer(ctx, *peerAddr)
}

func (n *NetworkManager) handleHandshake(stream network.Stream) {
	peerID := stream.Conn().RemotePeer()
	logger.Debugf("Processing handshake from peer_id=%s", peerID)
	authData, err := n.GetAuthData()
	if err != nil {
		logger.Warnf("Auth not configured - rejecting peer peer_id=%s", peerID)
		n.signalHandshakeDone(peerID)
		_ = stream.Reset()
		return
	}

	buf := make([]byte, 4096)
	nRead, err := stream.Read(buf)
	if err != nil {
		logger.Warnf("Handshake read failed for peer_id=%s error=%v", peerID, err)
		n.signalHandshakeDone(peerID)
		_ = stream.Reset()
		return
	}

	peerToken := string(buf[:nRead])
	if err := n.VerifyAuthToken(peerToken, peerID.String(), ""); err != nil {
		logger.Warnf("Handshake auth failed for peer_id=%s error=%v", peerID, err)
		n.signalHandshakeDone(peerID)
		_ = stream.Reset()
		return
	}

	_, err = stream.Write([]byte(authData))
	if err != nil {
		logger.Warnf("Handshake response failed for peer_id=%s error=%v", peerID, err)
		n.signalHandshakeDone(peerID)
		_ = stream.Reset()
		return
	}

	_ = stream.Close()
	n.signalHandshakeDone(peerID)
	logger.Infof("Handshake successful - authenticated peer_id=%s", peerID)
}

func (n *NetworkManager) signalHandshakeDone(peerID peer.ID) {
	n.handshakeMu.Lock()
	defer n.handshakeMu.Unlock()
	if n.pendingHandshakes == nil {
		return
	}

	doneCh, exists := n.pendingHandshakes[peerID]
	if !exists {
		return
	}
	delete(n.pendingHandshakes, peerID)
	close(doneCh)
}

func (n *NetworkManager) initiateHandshake(peerID peer.ID) {
	if !n.IsAuthEnabled() {
		return
	}

	authData, err := n.GetAuthData()
	if err != nil {
		logger.Warnf("Auth not configured - cannot initiate handshake with peer_id=%s", peerID)
		n.signalHandshakeDone(peerID)
		return
	}

	logger.Debugf("Initiating handshake with peer_id=%s", peerID)

	ctx, cancel := context.WithTimeout(n.getContext(), constants.HandshakeTimeout)
	defer cancel()

	stream, err := n.host.NewStream(ctx, peerID, protocol.ID(constants.HandshakeProtocolID))
	if err != nil {
		logger.Warnf("Failed to connect to peer for handshake peer_id=%s error=%v", peerID, err)
		n.signalHandshakeDone(peerID)
		return
	}
	defer stream.Close()

	_, err = stream.Write([]byte(authData))
	if err != nil {
		logger.Warnf("Failed to send auth token to peer_id=%s error=%v", peerID, err)
		n.signalHandshakeDone(peerID)
		return
	}

	buf := make([]byte, 4096)
	nRead, err := stream.Read(buf)
	if err != nil {
		logger.Warnf("Handshake timed out or failed for peer_id=%s error=%v", peerID, err)
		n.signalHandshakeDone(peerID)
		return
	}

	peerToken := string(buf[:nRead])
	if err := n.VerifyAuthToken(peerToken, peerID.String(), ""); err != nil {
		logger.Warnf("Peer failed auth verification peer_id=%s error=%v", peerID, err)
		_ = n.host.Network().ClosePeer(peerID)
		n.signalHandshakeDone(peerID)
		return
	}

	n.signalHandshakeDone(peerID)
	logger.Infof("Mutual auth successful with peer_id=%s", peerID)
}

// handleBlockStream serves a single block over the /raag/blocks/1.0.0 protocol.
// The remote peer sends the raw CID bytes; we look up the block locally and
// write the data back, then close the stream.
func (n *NetworkManager) handleBlockStream(stream network.Stream) {
	defer stream.Close()
	peerID := stream.Conn().RemotePeer()

	cidBytes, err := io.ReadAll(stream)
	if err != nil {
		logger.Warnf("handleBlockStream: read CID from peer=%s: %v", peerID, err)
		_ = stream.Reset()
		return
	}
	if len(cidBytes) == 0 {
		logger.Warnf("handleBlockStream: empty CID from peer=%s", peerID)
		_ = stream.Reset()
		return
	}

	data, ok := n.transferMgr.ServeBlock(cidBytes)
	if !ok {
		logger.Debugf("handleBlockStream: block not found for peer=%s", peerID)
		_ = stream.Reset()
		return
	}

	if _, err := stream.Write(data); err != nil {
		logger.Warnf("handleBlockStream: write block to peer=%s: %v", peerID, err)
	}
}

// ProvideSong chunks a local song and announces all blocks to the DHT.
// Call this when a new song is added to the local library so remote peers
// can fetch it via RequestSong.  Songs received via the legacy ShareSong /
// handleStream path are also announced automatically.
func (n *NetworkManager) ProvideSong(song metadata.Song) {
	if n.transferMgr == nil {
		return
	}
	ctx, cancel := context.WithTimeout(n.getContext(), 2*time.Minute)
	defer cancel()
	if err := n.transferMgr.Provide(ctx, song); err != nil {
		logger.Warnf("ProvideSong: failed to announce blocks for song=%s: %v", song.Title, err)
	}
}

// RequestSong initiates a chunked download of a remote song identified by its
// ordered block CIDs using the Phase-4 pull model (/raag/blocks/1.0.0).
// blockCIDBytes is a slice where each element is the raw CID bytes
// (cid.Cid.Bytes()) for that block, in order.
//
// This is the pull counterpart to ShareSong.  The remote node must have
// called ProvideSong (or received the file via the legacy push protocol,
// which triggers ProvideSong automatically) before this call succeeds.
//
// Returns the transfer handle; callers can poll t.Status for progress.
func (n *NetworkManager) RequestSong(song metadata.Song, blockCIDBytes [][]byte) (*transfer.Transfer, error) {
	if n.transferMgr == nil {
		return nil, fmt.Errorf("transfer manager not initialized")
	}

	cids := make([]cid.Cid, 0, len(blockCIDBytes))
	for i, b := range blockCIDBytes {
		c, err := cid.Cast(b)
		if err != nil {
			return nil, fmt.Errorf("invalid CID at index %d: %w", i, err)
		}
		cids = append(cids, c)
	}

	return n.transferMgr.Download(n.getContext(), song, cids)
}

// TransferManager returns the underlying transfer.Manager for advanced usage.
func (n *NetworkManager) TransferManager() *transfer.Manager {
	return n.transferMgr
}
