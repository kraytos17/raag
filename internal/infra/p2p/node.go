package p2p

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	protocols "github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

type P2PNode struct {
	host           host.Host
	bootstrapPeers []string
	identity       *IdentityManager
	streamPool     *StreamPool
	streamHandler  *protocols.StreamHandler
	syncHandler    *protocols.SyncHandler
	resolver       *P2PResolver
	scorer         *PeerScorer
	peerCache      *discovery.PeerCache
	mdns           *discovery.MdnsDiscovery
	peerMgr        *peerManager
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

	syncHandler := protocols.NewSyncHandler(libraryRepo, p2pHost.ID())
	streamHandler := protocols.NewStreamHandler(libraryRepo)
	streamPool := NewStreamPool(p2pHost, protocols.StreamProtocol)
	scorer := NewPeerScorer()

	peerCache := discovery.NewPeerCache(100)
	peerMgr := newPeerManager(p2pHost, peerCache, libraryRepo)
	resolver := NewP2PResolver(libraryRepo, streamPool, peerMgr, scorer)
	node := &P2PNode{
		host:           p2pHost,
		bootstrapPeers: cfg.BootstrapPeers,
		identity:       identity,
		streamPool:     streamPool,
		streamHandler:  streamHandler,
		syncHandler:    syncHandler,
		resolver:       resolver,
		scorer:         scorer,
		peerCache:      peerCache,
		peerMgr:        peerMgr,
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
	n.host.SetStreamHandler(protocols.SyncProtocol, n.syncHandler.Handle)
	mdns := discovery.NewMdnsDiscovery(n.host, func(pi peer.AddrInfo) {
		slog.Info("peer discovered", "peer", pi.ID)
		if !n.peerCache.Add(pi) {
			slog.Warn("peer cache full, could not add peer", "peer", pi.ID)
		}
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

func (n *P2PNode) SyncHandler() *protocols.SyncHandler {
	return n.syncHandler
}

func (n *P2PNode) StreamHandler() *protocols.StreamHandler {
	return n.streamHandler
}

type peerManager struct {
	host      host.Host
	peerCache *discovery.PeerCache
	library   app.LibraryRepository
	mu        sync.RWMutex
	manifests map[peer.ID]*pb.LibraryManifest
	caps      map[peer.ID]*pb.PeerCapabilities
}

func newPeerManager(h host.Host, peerCache *discovery.PeerCache, library app.LibraryRepository) *peerManager {
	return &peerManager{
		host:      h,
		peerCache: peerCache,
		library:   library,
		manifests: make(map[peer.ID]*pb.LibraryManifest),
		caps:      make(map[peer.ID]*pb.PeerCapabilities),
	}
}

func (pm *peerManager) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []peer.ID {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var owners []peer.ID
	for pid, manifest := range pm.manifests {
		if slices.Contains(manifest.TrackIds, string(trackID)) {
			owners = append(owners, pid)
		}
	}
	return owners
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
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if caps, ok := pm.caps[pid]; ok {
		return &domain.PeerCapabilities{
			SupportedCodecs:   caps.SupportedCodecs,
			SupportedBitrates: caps.SupportedBitrates,
			CanTranscode:      caps.CanTranscode,
			ProtocolVersion:   caps.ProtocolVersion,
		}
	}
	return &domain.PeerCapabilities{
		SupportedCodecs: app.AudioExtensions,
	}
}

func (pm *peerManager) FetchManifest(ctx context.Context, pid peer.ID) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	stream, err := pm.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return err
	}
	defer stream.Close()

	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_ManifestRequest{},
	}
	if err := wire.WriteMsg(stream, req); err != nil {
		return err
	}

	var resp pb.SyncResponse
	if err := wire.ReadMsg(stream, &resp); err != nil {
		return err
	}
	if manifest, ok := resp.GetPayload().(*pb.SyncResponse_Manifest); ok {
		pm.mu.Lock()
		pm.manifests[pid] = manifest.Manifest
		pm.mu.Unlock()
	}
	return nil
}

func (pm *peerManager) GetManifest(pid peer.ID) *pb.LibraryManifest {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.manifests[pid]
}
