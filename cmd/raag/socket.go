package main

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	appconfig "github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/network"
	"github.com/p-society/raag/internal/socket"
)

const SocketName = "daemon.sock"

type SocketServer struct {
	socketPath string
	nm         *network.NetworkManager
	listener   net.Listener
	stopCh     chan struct{}
	startTime  time.Time
}

func NewSocketServer(nm *network.NetworkManager) *SocketServer {
	socketPath := getSocketPath()
	return &SocketServer{
		socketPath: socketPath,
		nm:         nm,
		stopCh:     make(chan struct{}),
		startTime:  time.Now(),
	}
}

func getSocketPath() string {
	socketPath, err := appconfig.SocketPath()
	if err != nil {
		return filepath.Join(os.TempDir(), SocketName)
	}

	configDir, err := appconfig.Dir()
	if err != nil {
		return socketPath
	}

	os.MkdirAll(configDir, 0o700)
	return socketPath
}

func (s *SocketServer) Start() error {
	os.Remove(s.socketPath)
	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return err
	}

	s.listener = listener
	os.Chmod(s.socketPath, 0o600)
	logger.Infof("Daemon socket listening socket_path=%s", s.socketPath)

	go s.acceptLoop()
	return nil
}

func (s *SocketServer) acceptLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		default:
			conn, err := s.listener.Accept()
			if err != nil {
				continue
			}
			go s.handleConnection(conn)
		}
	}
}

func (s *SocketServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	for {
		var req socket.Request
		if err := decoder.Decode(&req); err != nil {
			return
		}

		resp := s.handleRequest(req)
		if err := encoder.Encode(resp); err != nil {
			return
		}
	}
}

func (s *SocketServer) handleRequest(req socket.Request) socket.Response {
	switch req.Command {
	case "peers list":
		return s.handlePeersList()
	case "peers info":
		return s.handlePeersInfo()
	case "network status":
		return s.handleNetworkStatus()
	case "network auth-key":
		return s.handleNetworkAuthKey()
	case "peers connect":
		return s.handlePeersConnect(req)
	case "peers disconnect":
		return s.handlePeersDisconnect(req)
	case "peers tracker":
		return s.handlePeersTracker(req)
	case "peers bootstrap":
		return s.handlePeersBootstrap(req)
	case "peers known":
		return s.handlePeersKnown()
	case "library list":
		return s.handleLibraryList()
	case "status":
		return s.handleStatus()
	case "shutdown":
		return s.handleShutdown()
	default:
		return socket.Response{
			Success: false,
			Error:   "Unknown command: " + req.Command,
		}
	}
}

func (s *SocketServer) handlePeersList() socket.Response {
	peers := s.nm.GetPeers()
	data := make([]socket.PeerInfo, len(peers))
	for i, p := range peers {
		data[i] = socket.PeerToPeerInfo(p)
	}
	return socket.Response{
		Success: true,
		Data:    data,
	}
}

func (s *SocketServer) handlePeersInfo() socket.Response {
	peerID := s.nm.GetPeerID()
	multiaddr := s.nm.GetMultiaddr()

	connectedPeers := s.nm.GetPeers()
	allKnownPeers := s.nm.GetAllKnownPeers()

	result := map[string]any{
		"self": map[string]string{
			"peer_id":   peerID.String(),
			"multiaddr": multiaddr,
		},
		"connected_peers":      connectedPeers,
		"connected_peer_count": len(connectedPeers),
		"known_peers":          allKnownPeers,
		"known_peer_count":     len(allKnownPeers),
	}
	return socket.Response{
		Success: true,
		Data:    result,
	}
}

func (s *SocketServer) handlePeersKnown() socket.Response {
	allKnownPeers := s.nm.GetAllKnownPeers()
	data := make([]socket.PeerInfo, len(allKnownPeers))
	for i, p := range allKnownPeers {
		data[i] = socket.PeerToPeerInfo(p)
	}
	return socket.Response{
		Success: true,
		Data:    data,
	}
}

func (s *SocketServer) handleLibraryList() socket.Response {
	songs := lib.ListSongs()
	data := make([]socket.LibrarySong, len(songs))
	for i, song := range songs {
		data[i] = socket.SongToLibrarySong(song)
	}
	return socket.Response{
		Success: true,
		Data:    data,
	}
}

func (s *SocketServer) handleStatus() socket.Response {
	uptime := time.Since(s.startTime)
	return socket.Response{
		Success: true,
		Data: socket.DaemonStatus{
			Running:   true,
			PeerCount: len(s.nm.GetPeers()),
			Connected: s.nm.IsOnline(),
			Uptime:    uptime.Truncate(time.Second).String(),
			Version:   "1.0.0",
		},
	}
}

func (s *SocketServer) handleNetworkStatus() socket.Response {
	state, err := s.nm.GetNetworkState()
	if err != nil {
		return socket.Response{Success: false, Error: err.Error()}
	}
	return socket.Response{
		Success: true,
		Data:    socket.NetworkStatusFromDiscovery(state),
	}
}

func (s *SocketServer) handleNetworkAuthKey() socket.Response {
	return socket.Response{
		Success: true,
		Data: map[string]any{
			"auth_public_key": s.nm.GetAuthPublicKey(),
		},
	}
}

func (s *SocketServer) handlePeersConnect(req socket.Request) socket.Response {
	if len(req.Args) != 1 {
		return socket.Response{Success: false, Error: "peers connect requires one multiaddr argument"}
	}

	addrInfo, err := peer.AddrInfoFromString(req.Args[0])
	if err != nil {
		return socket.Response{Success: false, Error: "invalid multiaddr: " + err.Error()}
	}
	if err := s.nm.Connect(context.Background(), *addrInfo); err != nil {
		return socket.Response{Success: false, Error: err.Error()}
	}
	return socket.Response{Success: true}
}

func (s *SocketServer) handlePeersDisconnect(req socket.Request) socket.Response {
	if len(req.Args) != 1 {
		return socket.Response{Success: false, Error: "peers disconnect requires one peer ID argument"}
	}

	peerID, err := peer.Decode(req.Args[0])
	if err != nil {
		return socket.Response{Success: false, Error: "invalid peer ID: " + err.Error()}
	}
	if err := s.nm.Disconnect(peerID); err != nil {
		return socket.Response{Success: false, Error: err.Error()}
	}
	return socket.Response{Success: true}
}

func (s *SocketServer) handlePeersTracker(req socket.Request) socket.Response {
	if len(req.Args) != 1 {
		return socket.Response{Success: false, Error: "peers tracker requires one URL argument"}
	}
	s.nm.UpdateTrackerURL(context.Background(), req.Args[0])
	return socket.Response{Success: true}
}

func (s *SocketServer) handlePeersBootstrap(req socket.Request) socket.Response {
	if len(req.Args) != 1 {
		return socket.Response{Success: false, Error: "peers bootstrap requires one multiaddr argument"}
	}
	if err := s.nm.AddBootstrapPeer(context.Background(), req.Args[0]); err != nil {
		return socket.Response{Success: false, Error: err.Error()}
	}
	return socket.Response{Success: true}
}

func (s *SocketServer) handleShutdown() socket.Response {
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.Stop()
	}()
	return socket.Response{Success: true}
}

func (s *SocketServer) Stop() {
	close(s.stopCh)
	if s.listener != nil {
		s.listener.Close()
	}
	os.Remove(s.socketPath)
}

func (s *SocketServer) GetSocketPath() string {
	return s.socketPath
}
