package p2p

import (
	"context"
	"log/slog"
	"sync"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	protocols "github.com/p-society/raag/internal/infra/p2p/protocols"
)

type P2PNode struct {
	host           host.Host
	bootstrapPeers []string
	identity       *IdentityManager
	streamPool     *StreamPool
	streamHandler  *protocols.StreamHandler
	resolver       *P2PResolver
	scorer         *PeerScorer
	peerCache      *discovery.PeerCache
	mdns           *discovery.MdnsDiscovery
	mu             sync.Mutex
	started        bool
}

type P2PNodeConfig struct {
	DataDir         string
	ListenAddrs     []string
	AnnounceAddrs   []string
	BootstrapPeers  []string
	MdnsServiceName string
}

func NewP2PNode(ctx context.Context, cfg P2PNodeConfig, libraryRepo app.LibraryRepository) (*P2PNode, error) {
	identity := NewIdentityManager(cfg.DataDir)
	privKey, _, err := identity.LoadOrCreate()
	if err != nil {
		return nil, err
	}

	p2pHost, err := NewHost(privKey, P2PConfig{
		ListenAddrs:     cfg.ListenAddrs,
		AnnounceAddrs:   cfg.AnnounceAddrs,
		BootstrapPeers:  cfg.BootstrapPeers,
		MdnsServiceName: cfg.MdnsServiceName,
	})
	if err != nil {
		return nil, err
	}

	streamHandler := protocols.NewStreamHandler(libraryRepo)
	streamPool := NewStreamPool(p2pHost, protocols.StreamProtocol)
	scorer := NewPeerScorer()
	peerCache := discovery.NewPeerCache(100)
	resolver := NewP2PResolver(libraryRepo, streamPool, &peerManager{
		host:      p2pHost,
		peerCache: peerCache,
	}, scorer)

	node := &P2PNode{
		host:           p2pHost,
		bootstrapPeers: cfg.BootstrapPeers,
		identity:       identity,
		streamPool:     streamPool,
		streamHandler:  streamHandler,
		resolver:       resolver,
		scorer:         scorer,
		peerCache:      peerCache,
	}
	return node, nil
}

func (n *P2PNode) Start(ctx context.Context, bus domain.EventBus) error {
	n.mu.Lock()
	if n.started {
		n.mu.Unlock()
		return nil
	}

	n.started = true
	n.mu.Unlock()
	n.host.SetStreamHandler(protocols.StreamProtocol, n.streamHandler.Handle)
	mdns := discovery.NewMdnsDiscovery(n.host, func(pi peer.AddrInfo) {
		slog.Info("peer discovered", "peer", pi.ID)
		n.peerCache.Add(pi)
		bus.Publish(ctx, domain.NewEvent(domain.EventPeerConnected, domain.PeerConnectedPayload{
			PeerID: pi.ID,
		}))
	})

	n.mdns = mdns
	if err := mdns.Start(); err != nil {
		return err
	}
	if err := BootstrapPeers(ctx, n.host, n.bootstrapPeers); err != nil {
		slog.Warn("bootstrap failed", "err", err)
	}

	slog.Info("P2P node started", "peer_id", n.host.ID())
	return nil
}

func (n *P2PNode) Stop(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.mdns != nil {
		n.mdns.Close()
	}

	n.streamPool.Close()
	n.host.Close()
	slog.Info("P2P node stopped")
	return nil
}

func (n *P2PNode) Host() host.Host {
	return n.host
}

func (n *P2PNode) ID() peer.ID {
	return n.host.ID()
}

func (n *P2PNode) Resolver() *P2PResolver {
	return n.resolver
}

func (n *P2PNode) StreamPool() *StreamPool {
	return n.streamPool
}

func (n *P2PNode) PeerCache() *discovery.PeerCache {
	return n.peerCache
}

type peerManager struct {
	host      host.Host
	peerCache *discovery.PeerCache
}

func (pm *peerManager) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []peer.ID {
	return pm.host.Network().Peers()
}

func (pm *peerManager) GetPeerScore(ctx context.Context, pid peer.ID) *domain.PeerScore {
	return nil
}

func (pm *peerManager) RecordSuccess(pid peer.ID) {}

func (pm *peerManager) RecordFailure(pid peer.ID) {}

func (pm *peerManager) IsBanned(pid peer.ID) bool {
	return false
}

func (pm *peerManager) GetPeerCapabilities(pid peer.ID) *domain.PeerCapabilities {
	return &domain.PeerCapabilities{
		SupportedCodecs: []string{"mp3", "flac", "ogg", "wav"},
	}
}
