package p2p

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	protocols "github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

type P2PNode struct {
	host            host.Host
	bootstrapPeers  []string
	identity        *IdentityManager
	streamPool      *StreamPool
	streamHandler   *protocols.StreamHandler
	syncHandler     *protocols.SyncHandler
	resolver        *P2PResolver
	scorer          *PeerScorer
	peerCache       *discovery.PeerCache
	mdns            *discovery.MdnsDiscovery
	peerMgr         *peerManager
	mdnsServiceName string
	shareManifest   bool
	pingService     *ping.PingService
	done            chan struct{}
	mu              sync.Mutex
	started         bool
}

type P2PNodeConfig struct {
	DataDir         string
	ListenAddrs     []string
	AnnounceAddrs   []string
	BootstrapPeers  []string
	MdnsServiceName string
	ShareManifest   bool
	ConnMgrLowMark  int
	ConnMgrHighMark int
	ConnMgrGrace    time.Duration
}

func NewP2PNode(cfg P2PNodeConfig, libraryRepo app.LibraryRepository) (*P2PNode, error) {
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
		ConnMgrLowMark:  cfg.ConnMgrLowMark,
		ConnMgrHighMark: cfg.ConnMgrHighMark,
		ConnMgrGrace:    cfg.ConnMgrGrace,
	})
	if err != nil {
		return nil, err
	}

	syncHandler := protocols.NewSyncHandler(libraryRepo, p2pHost.ID())
	syncHandler.SetAnnounceLibrary(cfg.ShareManifest)
	syncHandler.SetCapabilitiesProvider(&localCapabilities{
		libraryRepo: libraryRepo,
		peerID:      p2pHost.ID(),
	})

	streamHandler := protocols.NewStreamHandler(libraryRepo)
	streamPool := NewStreamPool(p2pHost, protocols.StreamProtocol)
	scorer := NewPeerScorer()

	peerCache := discovery.NewPeerCache(100)
	peerMgr := newPeerManager(p2pHost, peerCache, libraryRepo, scorer)
	resolver := NewP2PResolver(libraryRepo, streamPool, peerMgr, scorer, p2pHost)
	node := &P2PNode{
		host:            p2pHost,
		bootstrapPeers:  cfg.BootstrapPeers,
		identity:        identity,
		streamPool:      streamPool,
		streamHandler:   streamHandler,
		syncHandler:     syncHandler,
		resolver:        resolver,
		scorer:          scorer,
		peerCache:       peerCache,
		peerMgr:         peerMgr,
		mdnsServiceName: cfg.MdnsServiceName,
		shareManifest:   cfg.ShareManifest,
	}

	node.done = make(chan struct{})
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
	n.host.Network().Notify(&connNotifier{
		host:          n.host,
		streamHandler: n.streamHandler,
		syncHandler:   n.syncHandler,
		peerMgr:       n.peerMgr,
		bus:           bus,
		shareManifest: n.shareManifest,
		done:          n.done,
	})

	mdns := discovery.NewMdnsDiscovery(n.host, n.mdnsServiceName, func(pi peer.AddrInfo) {
		slog.Info("peer discovered via mDNS", "peer", pi.ID)
		if !n.peerCache.Add(pi) {
			slog.Warn("peer cache full, could not add peer", "peer", pi.ID)
		}
	})

	n.mdns = mdns
	if err := mdns.Start(); err != nil {
		return err
	}

	n.pingService = ping.NewPingService(n.host)
	go n.measureLatencyLoop()
	if err := BootstrapPeers(ctx, n.host, n.bootstrapPeers); err != nil {
		slog.Warn("bootstrap failed", "err", err)
	}

	slog.Info("P2P node started", "peer_id", n.host.ID())
	return nil
}

func (n *P2PNode) Stop(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	select {
	case <-n.done:
	default:
		close(n.done)
	}

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

type NetworkInfo struct {
	PeerID         string
	ListenAddrs    []string
	ConnectedPeers []peer.AddrInfo
}

func (n *P2PNode) NetworkInfo() NetworkInfo {
	var addrs []string
	for _, addr := range n.host.Addrs() {
		addrs = append(addrs, addr.String())
	}
	connected := n.host.Network().Peers()

	var peerInfos []peer.AddrInfo
	for _, p := range connected {
		if p == n.host.ID() {
			continue
		}
		if addrInfo := n.host.Peerstore().PeerInfo(p); len(addrInfo.Addrs) > 0 {
			peerInfos = append(peerInfos, addrInfo)
		}
	}

	return NetworkInfo{
		PeerID:         n.host.ID().String(),
		ListenAddrs:    addrs,
		ConnectedPeers: peerInfos,
	}
}

type connNotifier struct {
	host          host.Host
	streamHandler *protocols.StreamHandler
	syncHandler   *protocols.SyncHandler
	peerMgr       *peerManager
	bus           domain.EventBus
	shareManifest bool
	done          <-chan struct{}
}

func (cn *connNotifier) Connected(_ network.Network, conn network.Conn) {
	pid := conn.RemotePeer()
	cn.streamHandler.Allow(pid)
	cn.syncHandler.Allow(pid)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		var trackCount int
		var err error
		if cn.shareManifest {
			if err = cn.peerMgr.FetchManifest(ctx, pid); err != nil {
				slog.Warn("failed to fetch manifest", "peer", pid, "err", err)
			} else {
				manifest := cn.peerMgr.GetManifest(pid)
				if manifest != nil {
					trackCount = len(manifest.TrackIds)
					slog.Info("manifest received", "peer", pid, "tracks", trackCount)
				}
			}
		}
		if err = cn.peerMgr.FetchCapabilities(ctx, pid); err != nil {
			slog.Warn("failed to fetch capabilities", "peer", pid, "err", err)
		}
		if caps := cn.peerMgr.GetCapabilities(pid); caps != nil {
			slog.Info("peer connected with capabilities",
				"peer", pid,
				"tracks", trackCount,
				"codecs", caps.SupportedCodecs,
				"transcode", caps.CanTranscode)
		}
	}()

	cn.bus.Publish(context.Background(), domain.NewEvent(
		domain.EventPeerConnected,
		domain.PeerConnectedPayload{PeerID: pid},
	))
}

func (cn *connNotifier) Disconnected(_ network.Network, conn network.Conn) {
	pid := conn.RemotePeer()
	cn.streamHandler.Deny(pid)
	cn.syncHandler.Deny(pid)
	cn.bus.Publish(context.Background(), domain.NewEvent(
		domain.EventPeerDisconnected,
		domain.PeerDisconnectedPayload{PeerID: pid},
	))
}

func (cn *connNotifier) Listen(_ network.Network, addr ma.Multiaddr)      {}
func (cn *connNotifier) ListenClose(_ network.Network, addr ma.Multiaddr) {}
func (cn *connNotifier) OpenedStream(_ network.Network, s network.Stream) {}
func (cn *connNotifier) ClosedStream(_ network.Network, s network.Stream) {}

func (n *P2PNode) measureLatencyLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-n.done:
			return
		case <-ticker.C:
			n.measureAllPeersLatency()
		}
	}
}

func (n *P2PNode) measureAllPeersLatency() {
	peers := n.host.Network().Peers()
	for _, pid := range peers {
		if pid == n.host.ID() {
			continue
		}
		go func(p peer.ID) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			rtt, err := n.pingPeer(ctx, p)
			if err != nil {
				return
			}
			n.scorer.RecordLatency(p, rtt)
		}(pid)
	}
}

func (n *P2PNode) pingPeer(ctx context.Context, pid peer.ID) (time.Duration, error) {
	resultCh := n.pingService.Ping(ctx, pid)
	select {
	case result := <-resultCh:
		if result.Error != nil {
			return 0, result.Error
		}
		return result.RTT, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

type peerManager struct {
	host      host.Host
	peerCache *discovery.PeerCache
	library   app.LibraryRepository
	scorer    *PeerScorer
	mu        sync.RWMutex
	manifests map[peer.ID]*pb.LibraryManifest
	caps      map[peer.ID]*pb.PeerCapabilities
}

func newPeerManager(h host.Host, peerCache *discovery.PeerCache, library app.LibraryRepository, scorer *PeerScorer) *peerManager {
	return &peerManager{
		host:      h,
		peerCache: peerCache,
		library:   library,
		scorer:    scorer,
		manifests: make(map[peer.ID]*pb.LibraryManifest),
		caps:      make(map[peer.ID]*pb.PeerCapabilities),
	}
}

func (pm *peerManager) FindPeersWithTrack(ctx context.Context, trackID domain.TrackID) []peer.ID {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if len(pm.manifests) == 0 {
		slog.Warn("no peer manifests available - privacy settings may prevent sharing")
		return nil
	}

	var owners []peer.ID
	for pid, manifest := range pm.manifests {
		if slices.Contains(manifest.TrackIds, string(trackID)) {
			owners = append(owners, pid)
		}
	}
	return owners
}

func (pm *peerManager) GetTrackOwners(trackID string) []peer.ID {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var owners []peer.ID
	for pid, manifest := range pm.manifests {
		if slices.Contains(manifest.TrackIds, trackID) {
			owners = append(owners, pid)
		}
	}
	return owners
}

func (pm *peerManager) GetPeerScore(ctx context.Context, pid peer.ID) *domain.PeerScore {
	return nil
}

func (pm *peerManager) RecordFailure(pid peer.ID) {
	if pm.scorer != nil {
		pm.scorer.RecordFailure(pid)
	}
}

func (pm *peerManager) RecordSuccess(pid peer.ID) {
	if pm.scorer != nil {
		pm.scorer.RecordSuccess(pid)
	}
}

func (pm *peerManager) IsBanned(pid peer.ID) bool {
	if pm.scorer != nil {
		return pm.scorer.IsBanned(pid)
	}
	return false
}

func (pm *peerManager) GetPeerLatency(pid peer.ID) time.Duration {
	if pm.scorer != nil {
		return pm.scorer.AvgLatency(pid)
	}
	return 0
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
		SupportedCodecs: domain.AudioExtensions,
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

func (pm *peerManager) FetchCapabilities(ctx context.Context, pid peer.ID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	stream, err := pm.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return err
	}
	defer stream.Close()

	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_CapabilitiesRequest{},
	}
	if err := wire.WriteMsg(stream, req); err != nil {
		return err
	}

	var resp pb.SyncResponse
	if err := wire.ReadMsg(stream, &resp); err != nil {
		return err
	}
	if caps, ok := resp.GetPayload().(*pb.SyncResponse_Capabilities); ok {
		pm.mu.Lock()
		pm.caps[pid] = caps.Capabilities
		pm.mu.Unlock()
		slog.Debug("capabilities received", "peer", pid,
			"codecs", caps.Capabilities.SupportedCodecs,
			"transcode", caps.Capabilities.CanTranscode)
	}
	return nil
}

func (pm *peerManager) GetManifest(pid peer.ID) *pb.LibraryManifest {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.manifests[pid]
}

func (pm *peerManager) GetCapabilities(pid peer.ID) *pb.PeerCapabilities {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.caps[pid]
}

func (pm *peerManager) GetBestCodec(pid peer.ID, preferredCodec string) string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	caps := pm.caps[pid]
	if caps == nil {
		return preferredCodec
	}
	for _, codec := range []string{preferredCodec, "opus", "mp3", "flac"} {
		if slices.Contains(caps.SupportedCodecs, codec) {
			return codec
		}
	}
	return "mp3"
}

func (pm *peerManager) HasTrack(pid peer.ID, trackID string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	manifest := pm.manifests[pid]
	if manifest == nil {
		return false
	}
	return slices.Contains(manifest.TrackIds, trackID)
}

type localCapabilities struct {
	libraryRepo app.LibraryRepository
	peerID      peer.ID
}

func (lc *localCapabilities) GetLocalCapabilities() *pb.PeerCapabilities {
	return &pb.PeerCapabilities{
		SupportedCodecs:   domain.AudioExtensions,
		SupportedBitrates: domain.SupportedBitrates,
		ProtocolVersion:   "1.0.0",
		CanTranscode:      false,
	}
}

func (lc *localCapabilities) PeerID() peer.ID {
	return lc.peerID
}
