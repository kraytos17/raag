package network

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/p2p/host/peerstore/pstoremem"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/multiformats/go-multiaddr"
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

type serializedKey struct {
	Type string `json:"type"`
	Data []byte `json:"data"`
}

func loadOrGenerateIdentity() (crypto.PrivKey, error) {
	keyPath, err := config.IdentityKeyPath()
	if err != nil {
		logger.Warnf("Could not get identity key path, generating new key: %v", err)
		key, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, err
		}
		return key, nil
	}

	dir, err := config.Dir()
	if err != nil {
		logger.Warnf("Could not get config dir, generating new key: %v", err)
		key, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, err
		}
		return key, nil
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		if os.IsNotExist(err) {
			key, _, err := crypto.GenerateEd25519Key(rand.Reader)
			if err != nil {
				return nil, err
			}
			return key, nil
		}

		logger.Warnf("Could not open config root, generating new key: %v", err)
		key, _, err := crypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, err
		}
		return key, nil
	}

	relPath, err := filepath.Rel(dir, keyPath)
	if err != nil {
		logger.Warnf("Could not compute relative path, generating new key: %v", err)
		key, _, err := crypto.GenerateEd25519Key(rand.Reader)
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
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key pair: %w", err)
	}

	keyData, err := crypto.MarshalPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal key: %w", err)
	}

	sk := serializedKey{
		Type: "Ed25519",
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

type NetworkManager struct {
	host            host.Host
	ctx             context.Context
	cfg             *config.Config
	viper           *viper.Viper
	library         *library.Library
	musicDir        string
	store           *storage.Store
	Online          bool
	OnPeerJoin      func(peer.ID)
	OnPeerLeave     func(peer.ID)
	discovery       *discovery.Manager
	pingService     *ping.PingService
	peerConnectCh   chan struct{}
	connectionGater *RaagConnectionGater
	transferStack   *transfer.Stack
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
	if n.transferStack != nil {
		if err := n.transferStack.Close(); err != nil {
			logger.Warnf("Error closing transfer stack: %v", err)
		}
	}
	if dht := n.discovery.DHT(); dht != nil {
		if err := dht.Close(); err != nil {
			logger.Warnf("Error closing DHT: %v", err)
		}
	}
	return n.host.Close()
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
	ps, err = pstoremem.NewPeerstore()
	if err != nil {
		return nil, fmt.Errorf("failed to create peerstore: %w", err)
	}

	opts = append(opts, libp2p.Peerstore(ps))
	logger.Debugf("Using memory peerstore with Badger-backed persistence")
	if store != nil {
		psStore := storage.NewPeerStore(store.Peers)
		peers, err := psStore.LoadPeers(context.Background())
		if err != nil {
			logger.Warnf("Failed to load persisted peers: %v", err)
		} else {
			for _, info := range peers {
				if len(info.Addrs) > 0 {
					ps.AddAddrs(info.ID, info.Addrs, peerstore.PermanentAddrTTL)
				}
			}
			logger.Debugf("Loaded %d persisted peers from Badger", len(peers))
		}
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
		opts = append(opts, libp2p.EnableHolePunching())
		opts = append(opts, libp2p.NATPortMap())
		opts = append(opts, libp2p.EnableNATService())
		logger.Debugf("NAT traversal enabled: circuit relay, hole punching, UPnP, AutoNAT")
	} else {
		logger.Debugf("Using offline mode with limited transports")
		opts = append(opts, libp2p.DefaultTransports)
	}

	connectionGater := NewRaagConnectionGater()
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

	var peerStore *storage.PeerStore
	if store != nil {
		peerStore = storage.NewPeerStore(store.Peers)
	}

	discoveryMgr := discovery.NewManager(discovery.ManagerConfig{
		Host:            host,
		AuthSecret:      cfg.AuthSecret,
		MaxPeers:        maxPeers,
		ListenHost:      cfg.Host,
		Rendezvous:      cfg.Rendezvous,
		DHTEnabled:      cfg.DHTEnabled,
		BootstrapPeers:  cfg.BootstrapPeers,
		MDNSEnabled:     cfg.MDNSEnabled,
		MDNSServiceName: cfg.MDNSServiceName,
		PeerStore:       peerStore,
	})

	nm := &NetworkManager{
		host:            host,
		cfg:             cfg,
		viper:           v,
		library:         lib,
		musicDir:        musicDir,
		store:           store,
		discovery:       discoveryMgr,
		pingService:     pingService,
		peerConnectCh:   make(chan struct{}, 1),
		connectionGater: connectionGater,
	}

	host.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(_ network.Network, conn network.Conn) {
			pid := conn.RemotePeer()
			if pid == host.ID() {
				return
			}

			logger.Infof("Peer connected peer=%s addr=%s", pid, conn.RemoteMultiaddr())
			select {
			case nm.peerConnectCh <- struct{}{}:
			default:
			}

			if !nm.Online {
				nm.Online = true
				logger.Infof("Network: Online")
			}
			if nm.OnPeerJoin != nil {
				go nm.OnPeerJoin(pid)
			}
		},
		DisconnectedF: func(_ network.Network, conn network.Conn) {
			pid := conn.RemotePeer()
			logger.Infof("Peer disconnected peer=%s", pid)
			if nm.OnPeerLeave != nil {
				go nm.OnPeerLeave(pid)
			}
			if len(host.Network().Peers()) == 0 {
				nm.Online = false
				logger.Infof("Network: Offline")
			}
		},
	})
	return nm, nil
}

func newResourceManager() (network.ResourceManager, error) {
	scalingLimits := rcmgr.DefaultLimits
	libp2p.SetDefaultServiceLimits(&scalingLimits)

	limitConfig := scalingLimits.AutoScale()
	rm, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limitConfig))
	if err != nil {
		return nil, err
	}

	logger.Infof("Resource manager initialized with default limits")
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
	if err := n.discovery.Start(ctx); err != nil {
		logger.Errorf("Discovery failed to start error=%v", err)
	}
	if n.store != nil && n.transferStack == nil {
		dhtRouting := n.discovery.DHT()
		ts, err := transfer.NewStack(ctx, n.host, dhtRouting, n.store.Blocks)
		if err != nil {
			logger.Errorf("Failed to create transfer stack: %v", err)
		} else {
			n.transferStack = ts
			logger.Infof("Transfer stack initialized (Bitswap + UnixFS DAG)")
			if dhtRouting != nil {
				go n.runReprovider(ctx)
			}
			go n.ingestLibrary(ctx)
		}
	}

	n.discovery.SetSongAnnounceCallback(n.onRemoteSongAnnounce)
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

// BroadcastSongAdded announces a new song to the network via GossipSub
func (n *NetworkManager) BroadcastSongAdded(song metadata.Song) {
	if n.discovery == nil {
		return
	}

	var songCID string
	if n.transferStack != nil {
		c, err := n.transferStack.AddFile(n.getContext(), song.Path)
		if err != nil {
			logger.Warnf("AddFile for broadcast: %v", err)
		} else {
			songCID = c.String()
		}
	}

	info := discovery.SongInfo{
		Title:     song.Title,
		Artist:    song.Artist,
		Album:     song.Album,
		Hash:      song.Hash,
		Size:      song.Size,
		Extension: song.Extension,
		CID:       songCID,
	}
	n.discovery.PublishSongAnnounce("add", info)
}

// BroadcastSongRemoved announces a song removal to the network via GossipSub
func (n *NetworkManager) BroadcastSongRemoved(song metadata.Song) {
	if n.discovery == nil {
		return
	}
	info := discovery.SongInfo{
		Title:     song.Title,
		Artist:    song.Artist,
		Album:     song.Album,
		Hash:      song.Hash,
		Size:      song.Size,
		Extension: song.Extension,
	}
	n.discovery.PublishSongAnnounce("remove", info)
}

// FetchSong retrieves a song by CID from the network
func (n *NetworkManager) FetchSong(ctx context.Context, c cid.Cid, song metadata.Song) error {
	if n.transferStack == nil {
		return fmt.Errorf("transfer stack not initialized")
	}

	destPath := filepath.Join(n.musicDir, sanitizeTransferName(song.Title)+song.Extension)
	return n.transferStack.FetchFile(ctx, c, destPath)
}

// SetSongAnnounceHandler sets the callback for receiving song announcements from peers
func (n *NetworkManager) SetSongAnnounceHandler(handler func(peer.ID, discovery.LibraryAnnounceMessage)) {
	if n.discovery != nil {
		n.discovery.SetSongAnnounceCallback(handler)
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

func (n *NetworkManager) runReprovider(ctx context.Context) {
	dht := n.discovery.DHT()
	if dht == nil || n.transferStack == nil {
		return
	}

	logger.Infof("Reprovider started: re-announces all local CIDs every 22 hours")
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(22 * time.Hour):
			if n.transferStack == nil {
				return
			}

			keys, err := n.transferStack.Blockstore().AllKeysChan(ctx)
			if err != nil {
				logger.Warnf("reprovide failed to get keys: %v", err)
				continue
			}
			for c := range keys {
				if err := dht.Provide(ctx, c, true); err != nil {
					logger.Warnf("reprovide failed for %s: %v", c, err)
				}
			}
		}
	}
}

func (n *NetworkManager) ingestLibrary(ctx context.Context) {
	if n.transferStack == nil || n.library == nil {
		return
	}

	ingested := 0
	skipped := 0
	for song := range n.library.AllSongs() {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if song.CID != "" {
			c, err := cid.Decode(song.CID)
			if err == nil {
				has, _ := n.transferStack.HasBlock(ctx, c)
				if has {
					skipped++
					continue
				}
			}
		}

		c, err := n.transferStack.AddFile(ctx, song.Path)
		if err != nil {
			logger.Debugf("ingestLibrary: skipping %q: %v", song.Title, err)
			continue
		}

		n.library.UpdateCID(song.Hash, c.String())
		ingested++
	}
	logger.Infof("ingestLibrary: ingested=%d skipped=%d", ingested, skipped)
}

func (n *NetworkManager) onRemoteSongAnnounce(pid peer.ID, msg discovery.LibraryAnnounceMessage) {
	if msg.Action != "add" || msg.CID == "" {
		return
	}
	if n.transferStack == nil {
		return
	}

	c, err := cid.Decode(msg.CID)
	if err != nil {
		logger.Debugf("onRemoteSongAnnounce: invalid CID from %s: %v", pid, err)
		return
	}
	if has, _ := n.transferStack.HasBlock(n.getContext(), c); has {
		return
	}

	ext := msg.Song.Extension
	if ext == "" {
		ext = ".mp3"
	}

	destPath := filepath.Join(n.musicDir, sanitizeTransferName(msg.Song.Title)+ext)
	if _, err := os.Stat(destPath); err == nil {
		return
	}

	logger.Infof("Pre-fetching song announced by peer=%s title=%q cid=%s", pid, msg.Song.Title, c)
	go func() {
		fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := n.transferStack.FetchFile(fetchCtx, c, destPath); err != nil {
			logger.Warnf("Pre-fetch failed title=%q: %v", msg.Song.Title, err)
			return
		}
		if err := n.library.ScanMusicLibrary(n.musicDir); err != nil {
			logger.Warnf("Library rescan after pre-fetch failed: %v", err)
		}
		logger.Infof("Pre-fetch complete title=%q", msg.Song.Title)
	}()
}
