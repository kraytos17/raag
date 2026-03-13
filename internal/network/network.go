package network

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/discovery"
	"github.com/p-society/raag/internal/library"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/metadata"
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

	data, err := os.ReadFile(keyPath)
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

		logger.Infof("Loaded existing identity key from %s", keyPath)
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
		logger.Infof("Generated and saved new identity key to %s", keyPath)
	}
	return key, nil
}

type NetworkManager struct {
	host          host.Host
	cfg           *config.Config
	viper         *viper.Viper
	library       *library.Library
	musicDir      string
	peers         map[peer.ID]struct{}
	peersLock     sync.RWMutex
	Online        bool
	OnPeerJoin    func(peer.ID)
	OnPeerLeave   func(peer.ID)
	OnStateChange func(bool)
	discovery     *discovery.Manager
}

func NewNetwork(cfg *config.Config, v *viper.Viper, lib *library.Library, musicDir string) (*NetworkManager, error) {
	logger.Infof("Network config network=%v host=%s port=%d rendezvous=%s", cfg.Network, cfg.Host, cfg.Port, cfg.Rendezvous)

	prvKey, err := loadOrGenerateIdentity()
	if err != nil {
		return nil, fmt.Errorf("failed to load identity: %w", err)
	}

	var opts []libp2p.Option
	listenAddr := fmt.Sprintf("/ip4/%s/tcp/%d", cfg.Host, cfg.Port)

	sourceMultiAddr, _ := multiaddr.NewMultiaddr(listenAddr)
	opts = append(opts, libp2p.ListenAddrs(sourceMultiAddr), libp2p.Identity(prvKey))

	if cfg.Network {
		logger.Infof("Using networked mode with NAT traversal")
		opts = append(opts, libp2p.DefaultTransports)
		opts = append(opts, libp2p.EnableRelay())
		opts = append(opts, libp2p.EnableHolePunching())
		opts = append(opts, libp2p.NATPortMap())
		opts = append(opts, libp2p.EnableNATService())
		opts = append(opts, libp2p.ConnectionManager(NewConnectionManager(10, 100, 2*time.Minute)))
		logger.Infof("NAT traversal enabled: circuit relay, hole punching, UPnP, AutoNAT")
	} else {
		logger.Infof("Using offline mode with limited transports")
		opts = append(opts, libp2p.DefaultTransports)
		opts = append(opts, libp2p.ConnectionManager(NewConnectionManager(10, 15, time.Minute)))
	}

	host, err := libp2p.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create libp2p host: %w", err)
	}

	maxPeers := cfg.MaxPeers
	if maxPeers == 0 {
		maxPeers = constants.DefaultMaxPeers
	}

	discoveryMgr := discovery.NewManager(host, cfg.TrackerURL, maxPeers, cfg.Host, cfg.Rendezvous, cfg.DHTEnabled, cfg.BootstrapPeers)
	nm := &NetworkManager{
		host:      host,
		cfg:       cfg,
		viper:     v,
		library:   lib,
		musicDir:  musicDir,
		peers:     make(map[peer.ID]struct{}),
		discovery: discoveryMgr,
	}

	discoveryMgr.SetNetworkManager(nm)
	discoveryMgr.SetOnPeerSave(func(peers []peer.AddrInfo) {
		peerAddrs := discovery.AddrInfoStrings(peers)
		if len(peerAddrs) > 0 {
			if err := config.UpdateBootstrapPeers(v, peerAddrs); err != nil {
				logger.Warnf("Failed to update bootstrap_peers in config error=%v", err)
			} else {
				logger.Debugf("Updated bootstrap_peers in config count=%d", len(peerAddrs))
			}
		}
	})
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

func NewConnectionManager(low, high int, gracePeriod time.Duration) *connmgr.BasicConnMgr {
	cm, _ := connmgr.NewConnManager(low, high, connmgr.WithGracePeriod(gracePeriod))
	return cm
}

func (n *NetworkManager) Start(ctx context.Context) error {
	n.host.SetStreamHandler(protocol.ID(constants.ProtocolID), n.handleStream)
	if err := n.discovery.Start(ctx); err != nil {
		logger.Errorf("Discovery failed to start error=%v", err)
	}

	addrs := n.host.Addrs()
	if len(addrs) > 0 {
		logger.Infof("Your Raag Node Multiaddress address=%s", fmt.Sprintf("%s/p2p/%s", addrs[0], n.host.ID()))
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

// withPeersLock executes the given function with the peers lock held for reading
func (n *NetworkManager) withPeersLock(fn func(map[peer.ID]struct{})) {
	n.peersLock.RLock()
	defer n.peersLock.RUnlock()
	fn(n.peers)
}

// GetPeers returns information about currently connected peers
func (n *NetworkManager) GetPeers() []peer.AddrInfo {
	peers := make([]peer.AddrInfo, 0)
	n.withPeersLock(func(peersMap map[peer.ID]struct{}) {
		for peerID := range peersMap {
			if conns := n.host.Network().ConnsToPeer(peerID); len(conns) > 0 {
				peers = append(peers, peer.AddrInfo{
					ID:    peerID,
					Addrs: []multiaddr.Multiaddr{conns[0].RemoteMultiaddr()},
				})
			}
		}
	})
	return peers
}

// IsOnline returns true if there are any known peers
func (n *NetworkManager) IsOnline() bool {
	var isOnline bool
	n.withPeersLock(func(peersMap map[peer.ID]struct{}) {
		isOnline = len(peersMap) > 0
	})
	return isOnline
}

// GetPeerCount returns the number of known peers
func (n *NetworkManager) GetPeerCount() int {
	var count int
	n.withPeersLock(func(peersMap map[peer.ID]struct{}) {
		count = len(peersMap)
	})
	return count
}

func (n *NetworkManager) WaitForPeers(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if n.IsOnline() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return n.IsOnline()
}

func (n *NetworkManager) Connect(ctx context.Context, addrInfo peer.AddrInfo) error {
	if err := n.host.Connect(ctx, addrInfo); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	logger.Infof("Connecting to peer peer_id=%s", addrInfo.ID)
	if n.discovery != nil {
		n.discovery.RefreshTrackerRegistration(ctx)
	}
	return nil
}

func (n *NetworkManager) Disconnect(peerID peer.ID) error {
	if err := n.host.Network().ClosePeer(peerID); err != nil {
		return fmt.Errorf("failed to disconnect: %w", err)
	}

	n.peersLock.Lock()
	delete(n.peers, peerID)
	n.peersLock.Unlock()

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
	return !strings.Contains(addr, "/127.0.0.1/") && !strings.Contains(addr, "/0.0.0.0/")
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

func (n *NetworkManager) ShareSong(peerInfo *peer.AddrInfo, song metadata.Song) error {
	logger.Infof("ShareSong function called peer_info=%v song=%v", peerInfo, song)
	dataChan := make(chan []byte)
	go func() {
		defer close(dataChan)
		file, err := os.Open(song.Path)
		if err != nil {
			logger.Errorf("Error opening file error=%v", err)
			return
		}
		defer file.Close()

		buffer := make([]byte, 1024)
		for {
			n, err := file.Read(buffer)
			if err != nil && err != io.EOF {
				logger.Errorf("Error reading file error=%v", err)
				return
			}
			if n == 0 {
				break
			}
			dataChan <- buffer[:n]
		}
	}()

	stream, err := n.host.NewStream(context.Background(), peerInfo.ID, protocol.ID(constants.ProtocolID))
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}
	defer stream.Close()

	mdata := metadata.FormatMetadata(song)
	if _, err = stream.Write([]byte(mdata)); err != nil {
		return fmt.Errorf("failed to send song metadata: %w", err)
	}

	for data := range dataChan {
		if _, err = stream.Write(data); err != nil {
			return fmt.Errorf("failed to send song data: %w", err)
		}
	}

	logger.Infof("ShareSong function completed successfully")
	return nil
}

func (n *NetworkManager) handleStream(stream network.Stream) {
	defer stream.Close()

	peerID := stream.Conn().RemotePeer()
	logger.Infof("handleStream called peer_id=%s", peerID)

	buf := make([]byte, 1024)
	size, err := stream.Read(buf)
	if err != nil {
		if err == io.EOF {
			logger.Infof("Stream closed by peer before sending data peer_id=%s", peerID)
		} else {
			logger.Errorf("Error reading metadata from peer peer_id=%s error=%v", peerID, err)
		}
		return
	}

	if size == 0 {
		logger.Infof("Received empty stream from peer, ignoring peer_id=%s", peerID)
		return
	}

	mdata := string(buf[:size])
	logger.Infof("Received metadata metadata=%s", mdata)
	songInfo := strings.Split(mdata, "|")
	if len(songInfo) < 3 {
		logger.Errorf("Invalid song metadata")
		return
	}

	title := songInfo[0]
	pID := peerID.String()

	safeTitle := strings.ReplaceAll(title, "/", "_")
	safeTitle = strings.ReplaceAll(safeTitle, "\\", "_")
	fileName := fmt.Sprintf("%s_%s.mp3", pID, safeTitle)

	saveDir := n.musicDir
	if saveDir == "" {
		saveDir = "."
	}
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		logger.Errorf("Error creating directory error=%v", err)
		return
	}

	filePath := filepath.Join(saveDir, fileName)
	logger.Infof("Preparing to save file file_path=%s", filePath)

	file, err := os.Create(filePath)
	if err != nil {
		logger.Errorf("Error creating file error=%v", err)
		return
	}
	defer file.Close()

	logger.Infof("Copying song data from stream to file")
	bytesWritten, err := io.Copy(file, stream)
	if err != nil {
		logger.Errorf("Error saving song error=%v", err)
		return
	}

	logger.Infof("Song data saved bytes_written=%d", bytesWritten)
	logger.Infof("Successfully received and saved song title=%s peer_id=%s file_path=%s", title, peerID, filePath)
}

func (n *NetworkManager) handlePeerConnect(peerID peer.ID, addr multiaddr.Multiaddr) {
	n.notifyPeerConnected(peerID, addr.String())
}

// NotifyPeerConnected is called when a peer connection is established externally
func (n *NetworkManager) NotifyPeerConnected(peerID peer.ID, addr string) {
	n.notifyPeerConnected(peerID, addr)
}

func (n *NetworkManager) notifyPeerConnected(peerID peer.ID, addr string) {
	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	if peerID == n.host.ID() {
		return
	}
	if _, ok := n.peers[peerID]; !ok {
		n.peers[peerID] = struct{}{}
		logger.Infof("Peer connected peer_id=%s address=%s", peerID, addr)
		if n.discovery != nil {
			n.discovery.MarkPeerConnected(peerID)
		}
		if n.OnPeerJoin != nil {
			n.OnPeerJoin(peerID)
		}
		if !n.Online {
			n.Online = true
			logger.Infof("Network: Online - peers connected")
			if n.OnStateChange != nil {
				n.OnStateChange(true)
			}
		}
	}
}

func (n *NetworkManager) handlePeerDisconnect(peerID peer.ID, addr multiaddr.Multiaddr) {
	n.peersLock.Lock()
	defer n.peersLock.Unlock()

	if _, ok := n.peers[peerID]; ok {
		delete(n.peers, peerID)
		logger.Infof("Peer has disconnected peer_id=%s address=%s", peerID, addr.String())
		if n.OnPeerLeave != nil {
			n.OnPeerLeave(peerID)
		}
		if len(n.peers) == 0 {
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
