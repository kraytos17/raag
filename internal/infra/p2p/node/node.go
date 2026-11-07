package node

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/fsroot"
	"github.com/p-society/raag/internal/infra/observability"
	p2p "github.com/p-society/raag/internal/infra/p2p"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	protocols "github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/transcoder"
)

type Node struct {
	host             host.Host
	gater            *p2p.PeerGater
	bootstrapPeers   []string
	identity         *p2p.IdentityManager
	streamPool       *p2p.StreamPool
	streamHandler    *protocols.StreamHandler
	syncHandler      *protocols.SyncHandler
	resolver         *p2p.P2PResolver
	scorer           *p2p.PeerScorer
	peerCache        *discovery.PeerCache
	mdnsDiscovered   *discovery.TTLPeerCache
	mdnsService      peerDiscoveryService
	broadcastService peerDiscoveryService
	mdnsFactory      func(<-chan struct{}, discovery.PeerHandler, discovery.PeerHandler) peerDiscoveryService
	broadcastFactory func(<-chan struct{}, host.Host, int, string, discovery.PeerHandler, discovery.PeerHandler) peerDiscoveryService
	notifier         network.Notifiee
	peerMgr          p2p.PeerLifecycle
	mdnsServiceName  string
	admission        *protocols.AdmissionRegistry
	ctx              context.Context
	cancel           context.CancelCauseFunc
	lifecycleMu      sync.Mutex
	backgroundMu     sync.Mutex
	backgroundWG     sync.WaitGroup
	backgroundStop   bool
	shutdownDone     chan struct{}
	started          bool
	stopped          bool
	lanOnly          bool
	maxPeers         int
	maxKnownPeers    int
	chunkSize        int
	peerDataTTL      time.Duration
	broadcastPort    int
	broadcastTarget  string
	broadcastEnabled bool
	metrics          *observability.Metrics
	bus              domain.EventBus
}

// ErrNodeStopped reports an attempt to start a node after shutdown or a
// failed startup. A node owns non-restartable resources (host, stream pool,
// discovery caches), so callers must construct a new node to retry.
var ErrNodeStopped = errors.New("p2p node is stopped")

type peerDiscoveryService interface {
	Start() error
	Close() error
}

type Config struct {
	DataDir            string
	ListenAddrs        []string
	AnnounceAddrs      []string
	BootstrapPeers     []string
	MdnsServiceName    string
	ShareManifest      bool
	ConnMgrLowMark     int
	ConnMgrHighMark    int
	ConnMgrGrace       time.Duration
	ConnectionGater    *p2p.PeerGater
	LANOnly            bool
	MaxPeers           int
	StreamPort         int
	PerPeerRateLimit   int
	UploadBandwidth    int64
	CBFailureThreshold int
	CBCooldown         time.Duration
	MaxKnownPeers      int
	ChunkSize          int
	PeerDataTTL        time.Duration
	BroadcastEnabled   bool
	BroadcastPort      int
	BroadcastTarget    string
	LibraryPaths       []string
	Transcoder         *transcoder.Transcoder
	PeerRepo           app.PeerRepository
	Search             app.SearchHandler
	Metrics            *observability.Metrics
}

func New(cfg Config, libraryRepo app.LibraryRepository) (*Node, error) {
	identity := p2p.NewIdentityManager(cfg.DataDir)
	privKey, _, err := identity.LoadOrCreate()
	if err != nil {
		return nil, err
	}

	gater := cfg.ConnectionGater
	if gater == nil {
		gater = p2p.NewPeerGater()
	}
	if cfg.LANOnly {
		gater.SetLANOnly(true)
	}

	p2pHost, err := p2p.NewHost(privKey, p2p.P2PConfig{
		ListenAddrs:     cfg.ListenAddrs,
		AnnounceAddrs:   cfg.AnnounceAddrs,
		BootstrapPeers:  cfg.BootstrapPeers,
		MdnsServiceName: cfg.MdnsServiceName,
		ConnMgrLowMark:  cfg.ConnMgrLowMark,
		ConnMgrHighMark: cfg.ConnMgrHighMark,
		ConnMgrGrace:    cfg.ConnMgrGrace,
		LANOnly:         cfg.LANOnly,
		ConnectionGater: gater,
	})
	if err != nil {
		return nil, err
	}
	if cfg.LANOnly {
		if len(cfg.BootstrapPeers) > 0 {
			slog.Info("P2P LAN-only mode enabled – ignoring configured bootstrap peers",
				"original_count", len(cfg.BootstrapPeers))
		}

		cfg.BootstrapPeers = nil
		slog.Info("P2P running in pure LAN mDNS mode",
			"note", "bootstrap peers disabled, discovery limited to local network")
	}
	return newNodeWithHost(cfg, libraryRepo, identity, gater, p2pHost)
}

// NewWithHost wires a node around an already-created libp2p host. This is
// useful when the host's transport/lifecycle is managed by an embedding
// application; New remains the convenience constructor for standalone nodes.

func NewWithHost(cfg Config, libraryRepo app.LibraryRepository, gater *p2p.PeerGater, p2pHost host.Host) (*Node, error) {
	if gater == nil {
		gater = p2p.NewPeerGater()
	}
	if cfg.LANOnly {
		gater.SetLANOnly(true)
		cfg.BootstrapPeers = nil
	}
	return newNodeWithHost(cfg, libraryRepo, nil, gater, p2pHost)
}

// newNodeWithHost wires the node-owned components onto an already-created host.
// Keeping this seam lets tests construct real nodes with controlled hosts
// without reaching into the node's internal fields.

func newNodeWithHost(cfg Config, libraryRepo app.LibraryRepository, identity *p2p.IdentityManager, gater *p2p.PeerGater, p2pHost host.Host) (*Node, error) {
	syncHandler := protocols.NewSyncHandler(libraryRepo, cfg.Search, p2pHost.ID())
	syncHandler.SetAnnounceLibrary(cfg.ShareManifest)
	syncHandler.SetCapabilitiesProvider(&p2p.LocalCapabilities{
		LibraryRepo:     libraryRepo,
		PeerID:          p2pHost.ID(),
		CanTranscode:    cfg.Transcoder != nil,
		UploadBandwidth: cfg.UploadBandwidth,
	})

	streamHandler := protocols.NewStreamHandler(libraryRepo, fsroot.Open(cfg.LibraryPaths))
	streamHandler.SetRateLimiters(cfg.PerPeerRateLimit, cfg.UploadBandwidth)
	if cfg.Transcoder != nil {
		streamHandler.SetTranscoder(cfg.Transcoder)
	}

	streamPool := p2p.NewStreamPool(p2pHost, protocols.StreamProtocol)
	scorer := p2p.NewPeerScorer()

	peerCache := discovery.NewPeerCache(cfg.MaxKnownPeers)
	mdnsDiscovered := discovery.NewTTLPeerCache(cfg.MaxKnownPeers, discovery.MdnsDiscoveryTTL)
	peerMgr := p2p.NewPeerLifecycle(p2pHost, peerCache, libraryRepo, scorer, cfg.PeerDataTTL, cfg.PeerRepo)
	resolver := p2p.NewP2PResolver(libraryRepo, streamPool, peerMgr, scorer, p2pHost)

	admission := protocols.NewAdmissionRegistry()
	syncHandler.SetAdmissionRegistry(admission)
	streamHandler.SetAdmissionRegistry(admission)
	node := &Node{
		host:             p2pHost,
		gater:            gater,
		bootstrapPeers:   cfg.BootstrapPeers,
		identity:         identity,
		streamPool:       streamPool,
		streamHandler:    streamHandler,
		syncHandler:      syncHandler,
		resolver:         resolver,
		scorer:           scorer,
		peerCache:        peerCache,
		mdnsDiscovered:   mdnsDiscovered,
		peerMgr:          peerMgr,
		mdnsServiceName:  cfg.MdnsServiceName,
		admission:        admission,
		lanOnly:          cfg.LANOnly,
		maxPeers:         cfg.MaxPeers,
		maxKnownPeers:    cfg.MaxKnownPeers,
		chunkSize:        cfg.ChunkSize,
		peerDataTTL:      cfg.PeerDataTTL,
		broadcastPort:    cfg.BroadcastPort,
		broadcastTarget:  cfg.BroadcastTarget,
		broadcastEnabled: cfg.BroadcastEnabled,
		metrics:          cfg.Metrics,
		shutdownDone:     make(chan struct{}),
	}
	return node, nil
}

func (n *Node) Host() host.Host {
	return n.host
}

func (n *Node) ID() peer.ID {
	return n.host.ID()
}

func (n *Node) Resolver() *p2p.P2PResolver {
	return n.resolver
}

func (n *Node) StreamPool() *p2p.StreamPool {
	return n.streamPool
}

func (n *Node) PeerGater() *p2p.PeerGater {
	return n.gater
}

func (n *Node) BanPeer(pid peer.ID) {
	n.gater.Ban(pid)
	slog.Debug("peer banned", "peer", pid)
}

func (n *Node) UnbanPeer(pid peer.ID) {
	n.gater.Unban(pid)
	slog.Debug("peer unbanned", "peer", pid)
}

func (n *Node) PeerCache() *discovery.PeerCache {
	return n.peerCache
}

func (n *Node) SyncHandler() *protocols.SyncHandler {
	return n.syncHandler
}

func (n *Node) StreamHandler() *protocols.StreamHandler {
	return n.streamHandler
}

type NetworkInfo struct {
	PeerID          string
	ListenAddrs     []string
	DiscoveredPeers []peer.AddrInfo
	ConnectedPeers  []peer.AddrInfo
}
