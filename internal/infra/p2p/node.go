package p2p

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	protocols "github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/wire"
	pb "github.com/p-society/raag/proto/gen"
)

const codecMP3 = "mp3"

type P2PNode struct {
	host            host.Host
	gater           *PeerGater
	bootstrapPeers  []string
	identity        *IdentityManager
	streamPool      *StreamPool
	streamHandler   *protocols.StreamHandler
	syncHandler     *protocols.SyncHandler
	resolver        *P2PResolver
	scorer          *PeerScorer
	peerCache       *discovery.PeerCache
	mdnsDiscovered  *discovery.PeerCache
	mdns            *discovery.MdnsDiscovery
	peerMgr         *peerManager
	mdnsServiceName string
	shareManifest   bool
	admission       *protocols.AdmissionRegistry
	done            chan struct{}
	mu              sync.Mutex
	started         bool
	lanOnly         bool
	maxKnownPeers   int
	chunkSize       int
	peerDataTTL     time.Duration
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
	ConnectionGater *PeerGater
	LANOnly         bool
	MaxKnownPeers   int
	ChunkSize       int
	PeerDataTTL     time.Duration
}

func NewP2PNode(cfg P2PNodeConfig, libraryRepo app.LibraryRepository) (*P2PNode, error) {
	identity := NewIdentityManager(cfg.DataDir)
	privKey, _, err := identity.LoadOrCreate()
	if err != nil {
		return nil, err
	}

	gater := cfg.ConnectionGater
	if gater == nil {
		gater = NewPeerGater()
	}

	p2pHost, err := NewHost(privKey, P2PConfig{
		ListenAddrs:     cfg.ListenAddrs,
		AnnounceAddrs:   cfg.AnnounceAddrs,
		BootstrapPeers:  cfg.BootstrapPeers,
		MdnsServiceName: cfg.MdnsServiceName,
		ConnMgrLowMark:  cfg.ConnMgrLowMark,
		ConnMgrHighMark: cfg.ConnMgrHighMark,
		ConnMgrGrace:    cfg.ConnMgrGrace,
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

	syncHandler := protocols.NewSyncHandler(libraryRepo, p2pHost.ID())
	syncHandler.SetAnnounceLibrary(cfg.ShareManifest)
	syncHandler.SetCapabilitiesProvider(&localCapabilities{
		libraryRepo: libraryRepo,
		peerID:      p2pHost.ID(),
	})

	streamHandler := protocols.NewStreamHandler(libraryRepo)
	streamPool := NewStreamPool(p2pHost, protocols.StreamProtocol)
	scorer := NewPeerScorer()

	peerCache := discovery.NewPeerCache(cfg.MaxKnownPeers)
	mdnsDiscovered := discovery.NewPeerCache(cfg.MaxKnownPeers)
	peerMgr := newPeerManager(p2pHost, peerCache, libraryRepo, scorer, cfg.PeerDataTTL)
	resolver := NewP2PResolver(libraryRepo, streamPool, peerMgr, scorer, p2pHost)

	admission := protocols.NewAdmissionRegistry()
	syncHandler.SetAdmissionRegistry(admission)
	streamHandler.SetAdmissionRegistry(admission)
	node := &P2PNode{
		host:            p2pHost,
		gater:           gater,
		bootstrapPeers:  cfg.BootstrapPeers,
		identity:        identity,
		streamPool:      streamPool,
		streamHandler:   streamHandler,
		syncHandler:     syncHandler,
		resolver:        resolver,
		scorer:          scorer,
		peerCache:       peerCache,
		mdnsDiscovered:  mdnsDiscovered,
		peerMgr:         peerMgr,
		mdnsServiceName: cfg.MdnsServiceName,
		shareManifest:   cfg.ShareManifest,
		admission:       admission,
		lanOnly:         cfg.LANOnly,
		maxKnownPeers:   cfg.MaxKnownPeers,
		chunkSize:       cfg.ChunkSize,
		peerDataTTL:     cfg.PeerDataTTL,
	}

	scorer.node = node

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
		done:          n.done,
		streamPool:    n.streamPool,
		admission:     n.admission,
	})

	for _, conn := range n.host.Network().Conns() {
		pid := conn.RemotePeer()
		if n.peerMgr.IsBanned(pid) {
			_ = conn.Close()
			continue
		}

		pi := n.host.Peerstore().PeerInfo(pid)
		n.peerMgr.peerCache.Add(pi)
		go n.fetchPeerData(pid)
	}

	mdns := discovery.NewMdnsDiscoveryWithHandlers(
		n.done,
		n.host,
		n.mdnsServiceName,
		func(pi peer.AddrInfo) {
			n.mdnsDiscovered.Add(pi)
		},
		func(pi peer.AddrInfo) {
			slog.Info("peer discovered via mDNS", "peer", pi.ID)
		},
	)

	n.mdns = mdns
	if err := mdns.Start(); err != nil {
		return err
	}

	go n.measureLatencyLoop()
	if !n.lanOnly {
		if err := BootstrapPeers(ctx, n.host, n.bootstrapPeers); err != nil {
			slog.Warn("bootstrap failed", "err", err)
		}
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
		if err := n.mdns.Close(); err != nil {
			slog.Warn("failed to close mDNS", "error", err)
		}
	}

	n.streamPool.Close()
	if err := n.host.Close(); err != nil {
		slog.Warn("failed to close host", "error", err)
	}
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

func (n *P2PNode) PeerGater() *PeerGater {
	return n.gater
}

func (n *P2PNode) BanPeer(pid peer.ID) {
	n.gater.Ban(pid)
	slog.Debug("peer banned", "peer", pid)
}

func (n *P2PNode) UnbanPeer(pid peer.ID) {
	n.gater.Unban(pid)
	slog.Debug("peer unbanned", "peer", pid)
}

func (n *P2PNode) PeerCache() *discovery.PeerCache {
	return n.peerCache
}

// PeerStatus enriches a peer with live connection state and capabilities.
// The peer's score (if any) is preserved from the supplied info.
func (n *P2PNode) PeerStatus(info *domain.PeerInfo) *pb.Peer {
	if info == nil {
		return nil
	}
	pid := info.ID
	peer := convert.PeerInfoToProto(info)
	peer.Connected = n.host.Network().Connectedness(pid) == network.Connected
	if caps := n.peerMgr.GetPeerCapabilities(pid); caps != nil {
		peer.Capabilities = &pb.PeerCapabilities{
			SupportedCodecs:   caps.SupportedCodecs,
			SupportedBitrates: caps.SupportedBitrates,
			CanTranscode:      caps.CanTranscode,
			ProtocolVersion:   caps.ProtocolVersion,
		}
	}
	return peer
}

func (n *P2PNode) SyncHandler() *protocols.SyncHandler {
	return n.syncHandler
}

func (n *P2PNode) StreamHandler() *protocols.StreamHandler {
	return n.streamHandler
}

type NetworkInfo struct {
	PeerID          string
	ListenAddrs     []string
	DiscoveredPeers []peer.AddrInfo
	ConnectedPeers  []peer.AddrInfo
}

func (n *P2PNode) NetworkInfo() NetworkInfo {
	var addrs []string
	for _, addr := range n.host.Addrs() {
		addrs = append(addrs, addr.String())
	}

	connected := n.host.Network().Peers()
	connectedSet := make(map[peer.ID]struct{}, len(connected))
	for _, p := range connected {
		connectedSet[p] = struct{}{}
	}

	discovered := n.mdnsDiscovered.All()
	self := n.host.ID()
	for i := 0; i < len(discovered); i++ {
		if discovered[i].ID == self {
			discovered = append(discovered[:i], discovered[i+1:]...)
			i--
			continue
		}
		if _, ok := connectedSet[discovered[i].ID]; ok {
			discovered = append(discovered[:i], discovered[i+1:]...)
			i--
		}
	}

	var peerInfos []peer.AddrInfo
	for _, p := range connected {
		if p == n.host.ID() {
			continue
		}

		addrInfo := n.host.Peerstore().PeerInfo(p)
		peerInfos = append(peerInfos, addrInfo)
	}

	return NetworkInfo{
		PeerID:          n.host.ID().String(),
		ListenAddrs:     addrs,
		DiscoveredPeers: discovered,
		ConnectedPeers:  peerInfos,
	}
}

type connNotifier struct {
	host          host.Host
	streamHandler *protocols.StreamHandler
	syncHandler   *protocols.SyncHandler
	peerMgr       *peerManager
	bus           domain.EventBus
	done          <-chan struct{}
	streamPool    *StreamPool
	admission     *protocols.AdmissionRegistry
}

func (cn *connNotifier) Connected(_ network.Network, conn network.Conn) {
	pid := conn.RemotePeer()
	if cn.peerMgr.IsBanned(pid) {
		slog.Warn("rejecting connection from banned peer", "peer", pid)
		if err := conn.Close(); err != nil {
			slog.Debug("failed to close banned peer connection", "peer", pid, "err", err)
		}
		return
	}
	if len(cn.host.Network().ConnsToPeer(pid)) > 1 {
		return
	}

	pi := cn.host.Peerstore().PeerInfo(pid)
	cn.peerMgr.peerCache.Add(pi)
	cn.admission.Admit(pid)
	slog.Debug("peer connected", "peer", pid, "addr", conn.RemoteMultiaddr().String())
	go cn.peerMgr.FetchPeerData(context.Background(), pid)

	cn.bus.Publish(context.Background(), domain.NewEvent(
		domain.EventPeerConnected,
		domain.PeerConnectedPayload{
			PeerID:       pid,
			Addr:         conn.RemoteMultiaddr().String(),
			Capabilities: cn.peerMgr.GetPeerCapabilities(pid),
		},
	))
}

func (cn *connNotifier) Disconnected(_ network.Network, conn network.Conn) {
	pid := conn.RemotePeer()
	slog.Debug("peer disconnected", "peer", pid)
	cn.admission.Revoke(pid)
	cn.peerMgr.onPeerDisconnected(pid)
	if cn.streamPool != nil {
		cn.streamPool.DrainPeer(pid)
	}

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
	for _, pi := range n.peerCache.All() {
		latency := n.host.Peerstore().LatencyEWMA(pi.ID)
		if latency > 0 {
			n.scorer.RecordLatency(pi.ID, latency)
		}
	}
}

func (n *P2PNode) fetchPeerData(pid peer.ID) {
	n.peerMgr.FetchPeerData(context.Background(), pid)
}

type peerManager struct {
	host      host.Host
	peerCache *discovery.PeerCache
	library   app.LibraryRepository
	scorer    *PeerScorer
	mu        sync.RWMutex
	manifests map[peer.ID]*peerManifest
	caps      map[peer.ID]*peerCaps

	fetchCancelMu sync.RWMutex
	fetchCancel   map[peer.ID]context.CancelFunc
	ttl           time.Duration
}

type peerManifest struct {
	data    *pb.LibraryManifest
	addedAt time.Time
}

type peerCaps struct {
	data    *pb.PeerCapabilities
	addedAt time.Time
}

func newPeerManager(h host.Host, peerCache *discovery.PeerCache, library app.LibraryRepository, scorer *PeerScorer, ttl time.Duration) *peerManager {
	return &peerManager{
		host:        h,
		peerCache:   peerCache,
		library:     library,
		scorer:      scorer,
		manifests:   make(map[peer.ID]*peerManifest),
		caps:        make(map[peer.ID]*peerCaps),
		fetchCancel: make(map[peer.ID]context.CancelFunc),
		ttl:         ttl,
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
	for pid, pmf := range pm.manifests {
		if time.Since(pmf.addedAt) > pm.ttl {
			delete(pm.manifests, pid)
			continue
		}
		if slices.Contains(pmf.data.TrackIds, string(trackID)) {
			owners = append(owners, pid)
		}
	}
	return owners
}

func (pm *peerManager) GetTrackOwners(trackID string) []peer.ID {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var owners []peer.ID
	for pid, pmf := range pm.manifests {
		if time.Since(pmf.addedAt) > pm.ttl {
			delete(pm.manifests, pid)
			continue
		}
		if slices.Contains(pmf.data.TrackIds, trackID) {
			owners = append(owners, pid)
		}
	}
	return owners
}

func (pm *peerManager) GetPeerScore(ctx context.Context, pid peer.ID) *domain.PeerScore {
	if pm.scorer == nil {
		return nil
	}

	lat, avgBW, successes, failures, _ := pm.scorer.Snapshot(pid)
	s := domain.NewPeerScore(pid)
	s.AvgLatency = lat
	s.AvgBandwidth = avgBW
	s.SuccessCount = successes
	s.FailureCount = failures

	return s
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

	if pc, ok := pm.caps[pid]; ok && time.Since(pc.addedAt) < pm.ttl {
		return &domain.PeerCapabilities{
			SupportedCodecs:   pc.data.SupportedCodecs,
			SupportedBitrates: pc.data.SupportedBitrates,
			CanTranscode:      pc.data.CanTranscode,
			ProtocolVersion:   pc.data.ProtocolVersion,
		}
	} else if ok {
		delete(pm.caps, pid)
	}
	return &domain.PeerCapabilities{
		SupportedCodecs: domain.AudioExtensions,
	}
}

func resetStream(s network.Stream) {
	if err := s.Reset(); err != nil {
		slog.Debug("stream reset error", "err", err)
	}
}

func closeStream(s network.Stream) {
	if err := s.Close(); err != nil {
		slog.Debug("stream close error", "err", err)
	}
}

func (pm *peerManager) FetchManifest(ctx context.Context, pid peer.ID) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	stream, err := pm.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return err
	}

	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_ManifestRequest{},
	}
	if err := wire.WriteMsg(stream, req); err != nil {
		resetStream(stream)
		return err
	}

	var resp pb.SyncResponse
	if err := wire.ReadMsg(stream, &resp); err != nil {
		resetStream(stream)
		return err
	}

	switch payload := resp.GetPayload().(type) {
	case *pb.SyncResponse_Manifest:
		pm.mu.Lock()
		pm.manifests[pid] = &peerManifest{data: payload.Manifest, addedAt: time.Now()}
		pm.mu.Unlock()
		closeStream(stream)
		return nil
	case *pb.SyncResponse_Error:
		resetStream(stream)
		return errors.New(payload.Error.Message)
	default:
		resetStream(stream)
		return errors.New("unexpected sync response")
	}
}

func (pm *peerManager) FetchCapabilities(ctx context.Context, pid peer.ID) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	stream, err := pm.host.NewStream(ctx, pid, protocols.SyncProtocol)
	if err != nil {
		return err
	}

	req := &pb.SyncRequest{
		Payload: &pb.SyncRequest_CapabilitiesRequest{},
	}
	if err := wire.WriteMsg(stream, req); err != nil {
		resetStream(stream)
		return err
	}

	var resp pb.SyncResponse
	if err := wire.ReadMsg(stream, &resp); err != nil {
		resetStream(stream)
		return err
	}

	switch payload := resp.GetPayload().(type) {
	case *pb.SyncResponse_Capabilities:
		pm.mu.Lock()
		pm.caps[pid] = &peerCaps{data: payload.Capabilities, addedAt: time.Now()}
		pm.mu.Unlock()
		slog.Debug("capabilities received", "peer", pid,
			"codecs", payload.Capabilities.SupportedCodecs,
			"transcode", payload.Capabilities.CanTranscode)
		closeStream(stream)
		return nil
	case *pb.SyncResponse_Error:
		resetStream(stream)
		return errors.New(payload.Error.Message)
	default:
		resetStream(stream)
		return errors.New("unexpected sync response")
	}
}

func (pm *peerManager) GetManifest(pid peer.ID) *pb.LibraryManifest {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pmf, ok := pm.manifests[pid]; ok && time.Since(pmf.addedAt) < pm.ttl {
		return pmf.data
	} else if ok {
		delete(pm.manifests, pid)
	}
	return nil
}

func (pm *peerManager) GetCapabilities(pid peer.ID) *pb.PeerCapabilities {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	if pc, ok := pm.caps[pid]; ok && time.Since(pc.addedAt) < pm.ttl {
		return pc.data
	} else if ok {
		delete(pm.caps, pid)
	}
	return nil
}

func (pm *peerManager) FetchPeerData(ctx context.Context, pid peer.ID) {
	pm.fetchCancelMu.Lock()
	if existingCancel, ok := pm.fetchCancel[pid]; ok {
		existingCancel()
	}

	baseCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	pm.fetchCancel[pid] = cancel
	pm.fetchCancelMu.Unlock()

	var trackCount int
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for attempt := range 4 {
			err := pm.FetchManifest(baseCtx, pid)
			if err == nil {
				manifest := pm.GetManifest(pid)
				if manifest != nil {
					trackCount = len(manifest.TrackIds)
					slog.Info("manifest received", "peer", pid, "tracks", trackCount)
				}
				return
			}
			if err.Error() == "library sharing disabled" {
				slog.Debug("manifest not available", "peer", pid)
				return
			}
			if err.Error() != "permission denied" {
				slog.Warn("failed to fetch manifest", "peer", pid, "err", err)
				return
			}

			select {
			case <-baseCtx.Done():
				return
			case <-time.After(time.Duration(100*(attempt+1)) * time.Millisecond):
			}
		}
	}()
	go func() {
		defer wg.Done()
		for attempt := range 4 {
			err := pm.FetchCapabilities(baseCtx, pid)
			if err == nil {
				return
			}
			if err.Error() != "permission denied" {
				slog.Warn("failed to fetch capabilities", "peer", pid, "err", err)
				return
			}
			select {
			case <-baseCtx.Done():
				return
			case <-time.After(time.Duration(100*(attempt+1)) * time.Millisecond):
			}
		}
	}()

	wg.Wait()
	pm.fetchCancelMu.Lock()
	delete(pm.fetchCancel, pid)
	pm.fetchCancelMu.Unlock()

	if caps := pm.GetCapabilities(pid); caps != nil {
		slog.Info("peer connected with capabilities",
			"peer", pid,
			"tracks", trackCount,
			"codecs", caps.SupportedCodecs,
			"transcode", caps.CanTranscode)
	}
}

func (pm *peerManager) onPeerDisconnected(pid peer.ID) {
	pm.fetchCancelMu.Lock()
	if cancel, ok := pm.fetchCancel[pid]; ok {
		cancel()
		delete(pm.fetchCancel, pid)
	}

	pm.fetchCancelMu.Unlock()
	pm.mu.Lock()
	defer pm.mu.Unlock()

	delete(pm.manifests, pid)
	delete(pm.caps, pid)
	pm.peerCache.Remove(pid)
	if pm.scorer != nil {
		pm.scorer.ClearLatency(pid)
	}
}

func (pm *peerManager) GetBestCodec(pid peer.ID, preferredCodec string) string {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pc, ok := pm.caps[pid]; ok && time.Since(pc.addedAt) < pm.ttl {
		for _, codec := range []string{preferredCodec, "opus", codecMP3, "flac"} {
			if slices.Contains(pc.data.SupportedCodecs, codec) {
				return codec
			}
		}
	} else if ok {
		delete(pm.caps, pid)
	}
	return codecMP3
}

func (pm *peerManager) HasTrack(pid peer.ID, trackID string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pmf, ok := pm.manifests[pid]; ok && time.Since(pmf.addedAt) < pm.ttl {
		return slices.Contains(pmf.data.TrackIds, trackID)
	} else if ok {
		delete(pm.manifests, pid)
	}
	return false
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
