package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/auth"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
)

type Tracker struct {
	mu           sync.RWMutex
	peers        map[string]time.Time
	host         host.Host
	dht          *dht.IpfsDHT
	discovery    *routing.RoutingDiscovery
	httpPort     int
	libp2pPort   int
	relayEnabled bool
	dhtEnabled   bool
	tokenManager *auth.TokenManager
}

type TrackerConfig struct {
	HTTPPort       int
	Libp2pPort     int
	RelayEnabled   bool
	DHTEnabled     bool
	BootstrapPeers []string
}

func NewTracker(cfg TrackerConfig) *Tracker {
	if cfg.Libp2pPort == 0 {
		cfg.Libp2pPort = constants.DefaultPort
	}
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = constants.DefaultHTTPPort
	}

	return &Tracker{
		peers:        make(map[string]time.Time),
		libp2pPort:   cfg.Libp2pPort,
		httpPort:     cfg.HTTPPort,
		relayEnabled: cfg.RelayEnabled,
		dhtEnabled:   cfg.DHTEnabled,
		tokenManager: auth.NewTokenManager(),
	}
}

func (t *Tracker) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := t.initLibp2p(ctx); err != nil {
		return fmt.Errorf("failed to init libp2p: %w", err)
	}

	if t.dhtEnabled {
		if err := t.initDHT(ctx); err != nil {
			logger.Warnf("Failed to init DHT: %v", err)
		}
	}

	go t.cleanupOldPeers()

	logger.Infof("Tracker HTTP server starting on port %d", t.httpPort)
	logger.Infof("Tracker libp2p listening on %s", t.multiaddr())

	go func() {
		if err := http.ListenAndServe(fmt.Sprintf(":%d", t.httpPort), t); err != nil {
			logger.Errorf("HTTP server failed: %v", err)
		}
	}()

	<-ctx.Done()
	return nil
}

func (t *Tracker) initLibp2p(ctx context.Context) error {
	prvKey, _, err := crypto.GenerateKeyPair(crypto.RSA, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate key: %w", err)
	}

	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", t.libp2pPort)
	sourceMultiAddr, _ := multiaddr.NewMultiaddr(listenAddr)

	opts := []libp2p.Option{
		libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.Identity(prvKey),
		libp2p.ConnectionManager(t.newConnManager()),
	}

	if t.relayEnabled {
		opts = append(opts, libp2p.EnableRelay())
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to create host: %w", err)
	}

	t.host = h
	logger.Infof("Tracker libp2p initialized, ID: %s", t.host.ID())
	return nil
}

func (t *Tracker) newConnManager() *connmgr.BasicConnMgr {
	cm, _ := connmgr.NewConnManager(50, 100, connmgr.WithGracePeriod(time.Minute))
	return cm
}

func (t *Tracker) initDHT(ctx context.Context) error {
	var opts []dht.Option
	opts = append(opts, dht.Mode(dht.ModeServer))

	kademliaDHT, err := dht.New(ctx, t.host, opts...)
	if err != nil {
		return fmt.Errorf("failed to create DHT: %w", err)
	}

	t.dht = kademliaDHT
	if err := kademliaDHT.Bootstrap(ctx); err != nil {
		logger.Warnf("DHT bootstrap warning: %v", err)
	}

	t.discovery = routing.NewRoutingDiscovery(kademliaDHT)
	logger.Infof("Tracker DHT initialized, routing table size: %d", t.dht.RoutingTable().Size())
	return nil
}

func (t *Tracker) multiaddr() string {
	addrs := t.host.Addrs()
	if len(addrs) == 0 {
		return ""
	}
	return fmt.Sprintf("%s/p2p/%s", addrs[0], t.host.ID())
}

func (t *Tracker) HostID() string {
	if t.host == nil {
		return ""
	}
	return t.host.ID().String()
}

func (t *Tracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	logger.Infof("tracker request method=%s path=%s", r.Method, r.URL.Path)
	w.Header().Set("Content-Type", "application/json")

	switch r.URL.Path {
	case "/":
		t.handleRoot(w)
	case "/peers":
		t.handleGetPeers(w)
	case "/register":
		t.handleRegisterPeer(w, r)
	case "/addr":
		t.handleGetAddr(w)
	case "/health":
		t.handleHealth(w)
	case "/auth/token":
		t.handleGenerateToken(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (t *Tracker) handleRoot(w http.ResponseWriter) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"name":      "Raag Tracker",
		"version":   "2.0.0",
		"libp2p":    t.multiaddr(),
		"relay":     t.relayEnabled,
		"dht":       t.dhtEnabled,
		"auth":      "ed25519 signatures",
		"endpoints": []string{"/peers", "/register", "/addr", "/health", "/auth/token"},
	})
}

func (t *Tracker) handleGetAddr(w http.ResponseWriter) {
	resp := map[string]any{
		"libp2p_addr":   t.multiaddr(),
		"relay_enabled": t.relayEnabled,
		"dht_enabled":   t.dhtEnabled,
		"host_id":       t.HostID(),
	}

	if t.relayEnabled && t.host != nil {
		resp["relay_addr"] = fmt.Sprintf("/p2p/%s/p2p-circuit", t.host.ID())
	}

	json.NewEncoder(w).Encode(resp)
}

func (t *Tracker) handleGetPeers(w http.ResponseWriter) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var peerList []map[string]any
	for addr, lastSeen := range t.peers {
		peerInfo := map[string]any{
			"addr":      addr,
			"last_seen": lastSeen.Unix(),
		}

		if t.relayEnabled {
			peerInfo["relay_addr"] = t.getRelayAddr(addr)
		}

		peerList = append(peerList, peerInfo)
	}

	json.NewEncoder(w).Encode(peerList)
}

func (t *Tracker) getRelayAddr(peerAddr string) string {
	if t.host == nil {
		return ""
	}

	pi, err := peer.AddrInfoFromString(peerAddr)
	if err != nil {
		return ""
	}

	return fmt.Sprintf("/p2p/%s/p2p-circuit/p2p/%s", t.host.ID(), pi.ID)
}

func (t *Tracker) handleRegisterPeer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data struct {
		Addr     string `json:"addr"`
		AuthData string `json:"auth_data,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if data.Addr == "" {
		http.Error(w, "Missing addr", http.StatusBadRequest)
		return
	}

	if err := t.verifyAuth(data.AuthData); err != nil {
		logger.Warnf("Authentication failed for %s: %v", data.Addr, err)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	t.mu.Lock()
	t.peers[data.Addr] = time.Now()
	t.mu.Unlock()

	resp := map[string]any{
		"status":     "ok",
		"registered": true,
	}

	if t.relayEnabled {
		resp["relay_addr"] = t.getRelayAddr(data.Addr)
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
	logger.Infof("Peer registered: %s", data.Addr)
}

func (t *Tracker) verifyAuth(authData string) error {
	if authData == "" {
		return fmt.Errorf("authentication required")
	}

	token, err := auth.DeserializeToken(authData)
	if err != nil {
		return fmt.Errorf("invalid token format: %w", err)
	}

	valid, err := t.tokenManager.VerifyToken(token)
	if err != nil {
		return fmt.Errorf("token verification failed: %w", err)
	}
	if !valid {
		return fmt.Errorf("invalid token")
	}
	return nil
}

func (t *Tracker) handleGenerateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data struct {
		PeerID    string `json:"peer_id"`
		PublicKey string `json:"public_key"`
		Signature string `json:"signature"`
		Timestamp int64  `json:"timestamp"`
		ExpiresAt int64  `json:"expires_at"`
		Nonce     string `json:"nonce"`
	}
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	token := &auth.AuthToken{
		PeerID:    data.PeerID,
		PublicKey: data.PublicKey,
		Signature: data.Signature,
		Timestamp: data.Timestamp,
		ExpiresAt: data.ExpiresAt,
		Nonce:     data.Nonce,
	}

	t.tokenManager.AddAuthorizedPeer(token)
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (t *Tracker) handleHealth(w http.ResponseWriter) {
	status := map[string]any{
		"status":        "healthy",
		"peers_count":   len(t.peers),
		"libp2p_online": t.host != nil,
		"dht_online":    t.dht != nil,
	}
	if t.dht != nil {
		status["dht_peers"] = t.dht.RoutingTable().Size()
	}
	json.NewEncoder(w).Encode(status)
}

func (t *Tracker) cleanupOldPeers() {
	for {
		time.Sleep(constants.PeerCleanupInterval)
		t.mu.Lock()
		for addr, lastSeen := range t.peers {
			if time.Since(lastSeen) > constants.PeerTimeout {
				delete(t.peers, addr)
				logger.Infof("Removed stale peer: %s", addr)
			}
		}
		t.mu.Unlock()
	}
}

func StartServer(port int) {
	tracker := NewTracker(TrackerConfig{
		HTTPPort:     port,
		Libp2pPort:   constants.DefaultPort,
		RelayEnabled: true,
		DHTEnabled:   true,
	})

	if err := tracker.Start(); err != nil {
		logger.Errorf("Tracker failed: %v", err)
		os.Exit(1)
	}
}
