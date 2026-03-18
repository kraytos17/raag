package network

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
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
	host            host.Host
	ctx             context.Context
	cfg             *config.Config
	viper           *viper.Viper
	library         *library.Library
	musicDir        string
	Online          bool
	OnPeerJoin      func(peer.ID)
	OnPeerLeave     func(peer.ID)
	discovery       *discovery.Manager
	pingService     *ping.PingService
	peerConnectCh   chan struct{}
	connectionGater *RaagConnectionGater
	transferMgr     *transfer.Manager
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
		discovery:       discoveryMgr,
		pingService:     pingService,
		peerConnectCh:   make(chan struct{}, 1),
		connectionGater: connectionGater,
		transferMgr:     transfer.NewManager(host, nil, musicDir),
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
	n.host.SetStreamHandler(protocol.ID(constants.ShareProtocolID), n.handleStream)
	n.host.SetStreamHandler(protocol.ID(constants.BlockProtocolID), n.handleBlockStream)
	if err := n.discovery.Start(ctx); err != nil {
		logger.Errorf("Discovery failed to start error=%v", err)
	}

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

	meta := buildTransferMetadata(song, song.Size, song.Hash)
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
