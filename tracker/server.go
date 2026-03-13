package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/p-society/raag/internal/auth"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
)

type Tracker struct {
	mu            sync.RWMutex
	peers         map[string]registeredPeer
	httpPort      int
	libp2pPort    int
	host          host.Host
	relayEnabled  bool
	startedAt     time.Time
	tokenManager  *auth.TokenManager
	trustedKeyMgr *auth.TrustedKeyManager
}

type registeredPeer struct {
	PeerID   string
	Addrs    []string
	LastSeen time.Time
}

type TrackerConfig struct {
	HTTPPort       int
	Libp2pPort     int
	RelayEnabled   bool
	AuthPublicKeys []string
}

func NewTracker(cfg TrackerConfig) *Tracker {
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = constants.DefaultHTTPPort
	}
	if cfg.Libp2pPort == 0 {
		cfg.Libp2pPort = constants.DefaultPort
	}

	t := &Tracker{
		peers:        make(map[string]registeredPeer),
		httpPort:     cfg.HTTPPort,
		libp2pPort:   cfg.Libp2pPort,
		relayEnabled: cfg.RelayEnabled,
		startedAt:    time.Now(),
	}
	if len(cfg.AuthPublicKeys) > 0 {
		t.tokenManager = auth.NewTokenManager()
		t.trustedKeyMgr = auth.NewTrustedKeyManager()
		for _, key := range cfg.AuthPublicKeys {
			t.trustedKeyMgr.AddKey(key)
		}
		logger.Infof("Auth enabled with %d trusted key(s)", len(cfg.AuthPublicKeys))
	}
	return t
}

func (t *Tracker) Start() error {
	if t.relayEnabled {
		if err := t.initLibp2p(); err != nil {
			logger.Warnf("Failed to init libp2p (relay disabled): %v", err)
			t.relayEnabled = false
		}
	}

	go t.cleanupOldPeers()

	logger.Infof("Tracker HTTP server starting on port %d", t.httpPort)
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", t.httpPort),
		Handler: t,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf("HTTP server failed: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Infof("Tracker shutting down...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warnf("HTTP server shutdown error: %v", err)
	}
	if t.host != nil {
		t.host.Close()
	}
	return nil
}

func (t *Tracker) initLibp2p() error {
	listenAddr := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", t.libp2pPort)
	sourceMultiAddr, _ := multiaddr.NewMultiaddr(listenAddr)
	opts := []libp2p.Option{
		libp2p.ListenAddrs(sourceMultiAddr),
		libp2p.EnableRelay(),
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		return fmt.Errorf("failed to create libp2p host: %w", err)
	}

	t.host = h
	logger.Infof("Tracker libp2p initialized, ID: %s", t.host.ID())
	logger.Infof("Tracker listening on: %s", t.multiaddr())
	return nil
}

func (t *Tracker) multiaddr() string {
	if t.host == nil {
		return ""
	}

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

func (t *Tracker) RelayAddr() string {
	if t.host == nil || !t.relayEnabled {
		return ""
	}
	return fmt.Sprintf("/p2p/%s/p2p-circuit", t.host.ID())
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
	default:
		http.NotFound(w, r)
	}
}

func (t *Tracker) handleRoot(w http.ResponseWriter) {
	json.NewEncoder(w).Encode(map[string]any{
		"name":        "Raag Tracker",
		"description": "Peer registry with relay support for Raag P2P network",
		"version":     "1.0.0",
		"endpoints":   []string{"/peers", "/register", "/addr", "/health"},
	})
}

func (t *Tracker) handleGetAddr(w http.ResponseWriter) {
	resp := map[string]any{
		"relay_enabled": false,
		"host_id":       t.HostID(),
	}
	json.NewEncoder(w).Encode(resp)
}

func (t *Tracker) handleRegisterPeer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Addrs    []string `json:"addrs"`
		PeerID   string   `json:"peer_id"`
		AuthData string   `json:"auth_data,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}
	if len(req.Addrs) == 0 || req.PeerID == "" {
		http.Error(w, "Missing addrs or peer_id", http.StatusBadRequest)
		return
	}

	validatedAddrs := make([]string, 0, len(req.Addrs))
	for _, addr := range req.Addrs {
		addrInfo, err := peer.AddrInfoFromString(addr)
		if err != nil {
			logger.Warnf("Registration denied: invalid peer addr for peer %s: %v", req.PeerID, err)
			http.Error(w, "Invalid peer address", http.StatusBadRequest)
			return
		}
		if addrInfo.ID.String() != req.PeerID {
			logger.Warnf("Registration denied: peer_id mismatch request=%s addr=%s", req.PeerID, addrInfo.ID)
			http.Error(w, "peer_id does not match addr", http.StatusBadRequest)
			return
		}
		validatedAddrs = append(validatedAddrs, addr)
	}

	if t.trustedKeyMgr != nil && t.tokenManager != nil {
		if req.AuthData == "" {
			logger.Warnf("Registration denied: auth required but not provided for peer %s", req.PeerID)
			http.Error(w, "Authentication required", http.StatusUnauthorized)
			return
		}

		token, err := auth.DeserializeToken(req.AuthData)
		if err != nil {
			logger.Warnf("Registration denied: invalid auth token format for peer %s: %v", req.PeerID, err)
			http.Error(w, "Invalid auth token format", http.StatusUnauthorized)
			return
		}
		if token.PeerID != req.PeerID {
			logger.Warnf("Registration denied: token peer mismatch request=%s token=%s", req.PeerID, token.PeerID)
			http.Error(w, "Authentication failed", http.StatusUnauthorized)
			return
		}

		logger.Debugf("Verifying token: peer_id=%s token_pubkey=%s", token.PeerID, token.PublicKey)
		valid, err := t.trustedKeyMgr.VerifyAndCheckTrust(token, t.tokenManager)
		if err != nil || !valid {
			logger.Warnf("Registration denied: auth verification failed for peer %s: %v (token key: %s)", req.PeerID, err, token.PublicKey)
			http.Error(w, "Authentication failed", http.StatusUnauthorized)
			return
		}
		logger.Infof("Peer authenticated: %s (key: %s...)", req.PeerID, token.PublicKey[:16])
	} else if t.trustedKeyMgr == nil && req.AuthData != "" {
		logger.Debugf("Auth data provided but tracker has no trusted keys - allowing (auth disabled)")
	}

	t.mu.Lock()
	t.peers[req.PeerID] = registeredPeer{
		PeerID:   req.PeerID,
		Addrs:    validatedAddrs,
		LastSeen: time.Now(),
	}
	t.mu.Unlock()

	logger.Infof("Peer registered: %s (%d addrs)", req.PeerID, len(validatedAddrs))
	json.NewEncoder(w).Encode(map[string]any{
		"success": true,
		"message": "Peer registered successfully",
	})
}

func (t *Tracker) handleGetPeers(w http.ResponseWriter) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	peerList := make([]map[string]any, 0, len(t.peers))
	for _, record := range t.peers {
		peerInfo := map[string]any{
			"peer_id":   record.PeerID,
			"addrs":     record.Addrs,
			"last_seen": record.LastSeen.Unix(),
		}
		peerList = append(peerList, peerInfo)
	}
	json.NewEncoder(w).Encode(peerList)
}

func (t *Tracker) handleHealth(w http.ResponseWriter) {
	t.mu.RLock()
	peerCount := len(t.peers)
	t.mu.RUnlock()

	json.NewEncoder(w).Encode(map[string]any{
		"status":         "healthy",
		"uptime_seconds": int(time.Since(t.startedAt).Seconds()),
		"peers_count":    peerCount,
		"relay_enabled":  false,
	})
}

func (t *Tracker) cleanupOldPeers() {
	ticker := time.NewTicker(constants.PeerCleanupInterval)
	defer ticker.Stop()

	for range ticker.C {
		t.mu.Lock()
		now := time.Now()
		for peerID, record := range t.peers {
			if now.Sub(record.LastSeen) > constants.PeerTimeout {
				delete(t.peers, peerID)
				logger.Debugf("Removed stale peer: %s", peerID)
			}
		}
		t.mu.Unlock()
	}
}
