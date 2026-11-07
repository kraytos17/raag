package p2p

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/fsroot"
	"github.com/p-society/raag/internal/infra/observability"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	protocols "github.com/p-society/raag/internal/infra/p2p/protocols"
	"github.com/p-society/raag/internal/infra/transcoder"
	pb "github.com/p-society/raag/proto/gen"
)

const (
	codecMP3    = "mp3"
	codecFlac   = "flac"
	codecVorbis = "vorbis"
	codecPCM    = "pcm"
	codecAAC    = "aac"
	codecOpus   = "opus"
	codecWMA    = "wma"
)

type P2PNode struct {
	host             host.Host
	gater            *PeerGater
	bootstrapPeers   []string
	identity         *IdentityManager
	streamPool       *StreamPool
	streamHandler    *protocols.StreamHandler
	syncHandler      *protocols.SyncHandler
	resolver         *P2PResolver
	scorer           *PeerScorer
	peerCache        *discovery.PeerCache
	mdnsDiscovered   *discovery.TTLPeerCache
	mdnsService      peerDiscoveryService
	broadcastService peerDiscoveryService
	mdnsFactory      func(<-chan struct{}, discovery.PeerHandler, discovery.PeerHandler) peerDiscoveryService
	broadcastFactory func(<-chan struct{}, host.Host, int, string, discovery.PeerHandler, discovery.PeerHandler) peerDiscoveryService
	notifier         network.Notifiee
	peerMgr          *peerManager
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

// ErrP2PNodeStopped reports an attempt to start a node after shutdown or a
// failed startup. A node owns non-restartable resources (host, stream pool,
// discovery caches), so callers must construct a new node to retry.
var ErrP2PNodeStopped = errors.New("p2p node is stopped")

type peerDiscoveryService interface {
	Start() error
	Close() error
}

type P2PNodeConfig struct {
	DataDir            string
	ListenAddrs        []string
	AnnounceAddrs      []string
	BootstrapPeers     []string
	MdnsServiceName    string
	ShareManifest      bool
	ConnMgrLowMark     int
	ConnMgrHighMark    int
	ConnMgrGrace       time.Duration
	ConnectionGater    *PeerGater
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
	if cfg.LANOnly {
		gater.SetLANOnly(true)
	}

	p2pHost, err := NewHost(privKey, P2PConfig{
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

	syncHandler := protocols.NewSyncHandler(libraryRepo, cfg.Search, p2pHost.ID())
	syncHandler.SetAnnounceLibrary(cfg.ShareManifest)
	syncHandler.SetCapabilitiesProvider(&localCapabilities{
		libraryRepo:     libraryRepo,
		peerID:          p2pHost.ID(),
		canTranscode:    cfg.Transcoder != nil,
		uploadBandwidth: cfg.UploadBandwidth,
	})

	streamHandler := protocols.NewStreamHandler(libraryRepo, fsroot.Open(cfg.LibraryPaths))
	streamHandler.SetRateLimiters(cfg.PerPeerRateLimit, cfg.UploadBandwidth)
	if cfg.Transcoder != nil {
		streamHandler.SetTranscoder(cfg.Transcoder)
	}

	streamPool := NewStreamPool(p2pHost, protocols.StreamProtocol)
	scorer := NewPeerScorer()

	peerCache := discovery.NewPeerCache(cfg.MaxKnownPeers)
	mdnsDiscovered := discovery.NewTTLPeerCache(cfg.MaxKnownPeers, discovery.MdnsDiscoveryTTL)
	peerMgr := newPeerManager(p2pHost, peerCache, libraryRepo, scorer, cfg.PeerDataTTL, cfg.PeerRepo)
	resolver := NewP2PResolver(libraryRepo, streamPool, peerMgr, scorer, p2pHost)

	admission := protocols.NewAdmissionRegistry()
	syncHandler.SetAdmissionRegistry(admission)
	streamHandler.SetAdmissionRegistry(admission)
	node := &P2PNode{
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

func (n *P2PNode) Start(ctx context.Context, bus domain.EventBus) error {
	n.lifecycleMu.Lock()
	defer n.lifecycleMu.Unlock()

	if n.stopped {
		return ErrP2PNodeStopped
	}
	if n.started {
		return nil
	}

	n.ctx, n.cancel = context.WithCancelCause(ctx)
	n.bus = bus
	n.host.SetStreamHandler(protocols.StreamProtocol, n.streamHandler.Handle)
	n.host.SetStreamHandler(protocols.SyncProtocol, n.syncHandler.Handle)
	notifier := &connNotifier{
		ctx:             n.ctx,
		host:            n.host,
		streamHandler:   n.streamHandler,
		syncHandler:     n.syncHandler,
		peerMgr:         n.peerMgr,
		bus:             bus,
		streamPool:      n.streamPool,
		admission:       n.admission,
		maxPeers:        n.maxPeers,
		startBackground: n.startBackground,
		admittedPeers:   make(map[peer.ID]struct{}),
		peerConnections: make(map[peer.ID]int),
	}
	for _, pid := range n.host.Network().Peers() {
		notifier.admittedPeers[pid] = struct{}{}
		notifier.peerConnections[pid] = len(n.host.Network().ConnsToPeer(pid))
	}

	n.notifier = notifier
	n.host.Network().Notify(n.notifier)
	for _, conn := range n.host.Network().Conns() {
		pid := conn.RemotePeer()
		if n.peerMgr.IsBanned(pid) {
			_ = conn.Close()
			continue
		}

		pi := n.host.Peerstore().PeerInfo(pid)
		n.peerMgr.peerCache.Add(pi)
		n.startBackground(func() { n.peerMgr.FetchPeerData(n.ctx, pid) })
	}

	onDiscovered := func(pi peer.AddrInfo) {
		n.mdnsDiscovered.Add(pi)
	}
	onMDNSConnected := func(pi peer.AddrInfo) {
		slog.Info("peer discovered via mDNS", "peer", pi.ID)
	}

	var mdns peerDiscoveryService
	if n.mdnsFactory != nil {
		mdns = n.mdnsFactory(n.ctx.Done(), onDiscovered, onMDNSConnected)
	} else {
		mdns = discovery.NewMdnsDiscoveryWithHandlers(
			n.ctx.Done(), n.host, n.mdnsServiceName, onDiscovered, onMDNSConnected,
		)
	}

	n.mdnsService = mdns
	if err := mdns.Start(); err != nil {
		return n.failStart(ctx, err)
	}
	if n.broadcastEnabled {
		onBroadcastConnected := func(pi peer.AddrInfo) {
			slog.Info("peer discovered via broadcast", "peer", pi.ID)
		}

		var broadcast peerDiscoveryService
		if n.broadcastFactory != nil {
			broadcast = n.broadcastFactory(n.ctx.Done(), n.host, n.broadcastPort, n.broadcastTarget,
				onDiscovered, onBroadcastConnected)
		} else {
			broadcast = discovery.NewBroadcastDiscovery(
				n.ctx.Done(), n.host, n.broadcastPort, n.broadcastTarget,
				onDiscovered, onBroadcastConnected,
			)
		}

		n.broadcastService = broadcast
		if err := broadcast.Start(); err != nil {
			return n.failStart(ctx, err)
		}
	}

	n.startBackground(n.measureLatencyLoop)
	n.startBackground(n.refreshManifestsLoop)
	if !n.lanOnly {
		n.startBackground(func() {
			if err := BootstrapPeers(n.ctx, n.host, n.bootstrapPeers); err != nil {
				slog.Warn("bootstrap failed", "err", err)
			}
		})
	}

	n.started = true
	slog.Info("P2P node started", "peer_id", n.host.ID())
	return nil
}

func (n *P2PNode) failStart(ctx context.Context, startErr error) error {
	if stopErr := n.stopLocked(ctx); stopErr != nil {
		return errors.Join(startErr, stopErr)
	}
	return startErr
}

func (n *P2PNode) Stop(ctx context.Context) error {
	n.lifecycleMu.Lock()
	defer n.lifecycleMu.Unlock()
	return n.stopLocked(ctx)
}

// stopLocked is shared by normal shutdown and startup rollback. The caller
// must hold lifecycleMu.
func (n *P2PNode) stopLocked(ctx context.Context) error {
	if n.shutdownDone == nil {
		n.shutdownDone = make(chan struct{})
	}
	if n.stopped {
		select {
		case <-n.shutdownDone:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	n.stopped = true
	n.started = false
	if n.cancel != nil {
		n.cancel(errors.New("p2p node stopped"))
	}

	n.backgroundMu.Lock()
	n.backgroundStop = true
	n.backgroundMu.Unlock()
	// Stop discovery sources before closing the host. Their Close methods unblock
	// their receive loops and ensure they cannot enqueue more peer work.
	if n.mdnsService != nil {
		if err := n.mdnsService.Close(); err != nil {
			slog.Warn("failed to close mDNS", "error", err)
		}
	}
	if n.broadcastService != nil {
		if err := n.broadcastService.Close(); err != nil {
			slog.Warn("failed to close broadcast discovery", "error", err)
		}
	}
	if n.notifier != nil {
		n.host.Network().StopNotify(n.notifier)
		n.notifier = nil
	}

	n.mdnsDiscovered.Close()
	n.streamPool.Close()
	if err := n.host.Close(); err != nil {
		slog.Warn("failed to close P2P host", "error", err)
	}
	go func() {
		n.backgroundWG.Wait()
		if tr := n.streamHandler.Transcoder(); tr != nil {
			tr.Cleanup()
		}
		slog.Info("P2P node stopped")
		close(n.shutdownDone)
	}()

	select {
	case <-n.shutdownDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startBackground tracks node-owned work so shutdown can cancel and wait for
// it. Once shutdown begins, no additional goroutines are admitted.
func (n *P2PNode) startBackground(work func()) bool {
	n.backgroundMu.Lock()
	if n.backgroundStop {
		n.backgroundMu.Unlock()
		return false
	}

	n.backgroundWG.Add(1)
	n.backgroundMu.Unlock()
	go func() {
		defer n.backgroundWG.Done()
		work()
	}()
	return true
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

// Peers returns the IDs of currently connected peers (excluding self).
func (n *P2PNode) Peers() []peer.ID {
	connected := n.host.Network().Peers()
	self := n.host.ID()
	out := make([]peer.ID, 0, len(connected))
	for _, p := range connected {
		if p != self {
			out = append(out, p)
		}
	}
	return out
}

// DiscoveredPeers returns peers discovered via mDNS that are not yet connected.
func (n *P2PNode) DiscoveredPeers() []peer.AddrInfo {
	return n.mdnsDiscovered.All()
}

// AddDiscovered records a discovered peer (e.g. from mDNS) so it is visible via
// DiscoveredPeers/ListPeers.
func (n *P2PNode) AddDiscovered(pi peer.AddrInfo) {
	n.mdnsDiscovered.Add(pi)
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
	if n.peerMgr != nil {
		if caps := n.peerMgr.GetPeerCapabilities(pid); caps != nil {
			peer.Capabilities = &pb.PeerCapabilities{
				SupportedCodecs:   caps.SupportedCodecs,
				SupportedBitrates: caps.SupportedBitrates,
				CanTranscode:      caps.CanTranscode,
				ProtocolVersion:   caps.ProtocolVersion,
			}
		}
	}
	// Overlay the live scorer snapshot so latency/bandwidth/score reflect the
	// most recent measurement
	n.overlayLiveScore(peer, pid)
	return peer
}

// overlayLiveScore merges the in-memory scorer's live snapshot into a peer's
// proto, filling latency/bandwidth/failure/success and recomputing the score.
func (n *P2PNode) overlayLiveScore(peer *pb.Peer, pid peer.ID) {
	latency, bandwidth, successes, failures, _ := n.scorer.Snapshot(pid)
	score := n.scorer.Score(pid)
	if peer.Score == nil {
		peer.Score = &pb.PeerScore{PeerId: pid.String()}
	}

	peer.Score.AvgLatencyMs = float64(latency.Milliseconds())
	peer.Score.AvgBandwidth = bandwidth
	peer.Score.SuccessCount = int32(successes)
	peer.Score.FailureCount = int32(failures)
	peer.Score.Score = score
	if peer.Score.LastSeen == 0 {
		peer.Score.LastSeen = time.Now().Unix()
	}
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
	ctx             context.Context
	host            host.Host
	streamHandler   *protocols.StreamHandler
	syncHandler     *protocols.SyncHandler
	peerMgr         *peerManager
	bus             domain.EventBus
	streamPool      *StreamPool
	admission       *protocols.AdmissionRegistry
	maxPeers        int
	startBackground func(func()) bool
	admissionMu     sync.Mutex
	admittedPeers   map[peer.ID]struct{}
	peerConnections map[peer.ID]int
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

	cn.admissionMu.Lock()
	if cn.admittedPeers == nil {
		cn.admittedPeers = make(map[peer.ID]struct{})
	}
	if cn.peerConnections == nil {
		cn.peerConnections = make(map[peer.ID]int)
	}
	if cn.peerConnections[pid] == 0 {
		if cn.maxPeers > 0 && len(cn.admittedPeers) >= cn.maxPeers {
			cn.admissionMu.Unlock()
			slog.Warn("peer connection rejected: max peers reached", "peer", pid, "max_peers", cn.maxPeers)
			if err := conn.Close(); err != nil {
				slog.Debug("failed to close excess peer connection", "peer", pid, "err", err)
			}
			return
		}
		cn.admittedPeers[pid] = struct{}{}
	}

	cn.peerConnections[pid]++
	duplicateConnection := cn.peerConnections[pid] > 1
	cn.admissionMu.Unlock()
	if duplicateConnection {
		return
	}

	pi := cn.host.Peerstore().PeerInfo(pid)
	cn.peerMgr.peerCache.Add(pi)
	// Admission is intentionally NOT granted here. A peer becomes admitted only
	// after it completes manifest exchange (SyncHandler.Handle grants it on a
	// ManifestRequest). Granting it eagerly on connect would make the chunk /
	// track-detail / remote-search guards meaningless.
	slog.Debug("peer connected", "peer", pid, "addr", conn.RemoteMultiaddr().String())
	cn.peerMgr.persist(pid)
	if cn.startBackground != nil {
		cn.startBackground(func() { cn.peerMgr.FetchPeerData(cn.ctx, pid) })
	}

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
	cn.admissionMu.Lock()
	remaining, ok := cn.peerConnections[pid]
	if !ok {
		// Untracked connection: rejected, banned, or never counted. Nothing
		// was started for it, so there is nothing to tear down and no event
		// to publish.
		cn.admissionMu.Unlock()
		return
	}

	remaining--
	if remaining <= 0 {
		delete(cn.peerConnections, pid)
		delete(cn.admittedPeers, pid)
	} else {
		cn.peerConnections[pid] = remaining
	}

	cn.admissionMu.Unlock()
	if remaining > 0 {
		return
	}

	slog.Debug("peer disconnected", "peer", pid)
	cn.admission.Revoke(pid)
	cn.peerMgr.onPeerDisconnected(pid)
	cn.peerMgr.persist(pid)
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
	runTickerLoop(n.ctx, 30*time.Second, n.measureAllPeersLatency)
}

// manifestRefreshInterval is how often peer library manifests are re-fetched so
// library changes propagate well under the 24h PeerDataTTL.
const manifestRefreshInterval = 5 * time.Minute

// refreshManifestsLoop periodically re-fetches manifests from connected peers
// so their libraries stay fresh. FetchPeerData is idempotent and cancels any
// prior in-flight fetch per peer, so overlapping ticks are safe.
func (n *P2PNode) refreshManifestsLoop() {
	runTickerLoop(n.ctx, manifestRefreshInterval, n.refreshAllManifests)
}

// runTickerLoop invokes work once per interval until ctx is canceled. Shared by
// the latency and manifest refresh loops so their timing can be tested directly.
func runTickerLoop(ctx context.Context, interval time.Duration, work func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			work()
		}
	}
}

// refreshAllManifests re-fetches manifests from all currently connected peers.
// Exposed as a method so the loop's work is unit-testable.
func (n *P2PNode) refreshAllManifests() {
	ctx := n.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	for _, pid := range n.Peers() {
		n.peerMgr.FetchPeerData(ctx, pid)
	}
}

func (n *P2PNode) measureAllPeersLatency() {
	if n.metrics != nil && n.metrics.ConnectedPeers != nil {
		n.metrics.ConnectedPeers.Set(float64(len(n.Peers())))
	}

	// Actively probe connected peers with a ping RTT; fall back to libp2p's
	// passive LatencyEWMA for known-but-unconnected peers. A peer that is
	// connected but fails ping is skipped (stale EWMA from an old connection
	// would mislead the scorer).
	connected := make(map[peer.ID]bool, len(n.Peers()))
	for _, pid := range n.Peers() {
		connected[pid] = true
		n.probePeerLatency(pid)
	}
	for _, pi := range n.peerCache.All() {
		if connected[pi.ID] {
			continue
		}

		latency := n.host.Peerstore().LatencyEWMA(pi.ID)
		if latency > 0 {
			n.scorer.RecordLatency(pi.ID, latency)
		}
	}
	if n.bus == nil {
		return
	}
	for _, pid := range n.Peers() {
		score := n.scorer.Score(pid)
		n.bus.Publish(context.Background(), domain.NewEvent(
			domain.EventPeerScoreUpdated,
			domain.PeerScoreUpdatedPayload{PeerID: pid, Score: score},
		))
	}
}

// latencyProbeTimeout bounds each active ping so a slow peer can't stall the
// whole measurement pass.
const latencyProbeTimeout = 3 * time.Second

// probePeerLatency actively measures a connected peer's round-trip time with a
// libp2p ping and records it to the scorer. Failures are best-effort: a failed
// ping simply leaves the previous recorded latency in place.
func (n *P2PNode) probePeerLatency(pid peer.ID) {
	ctx, cancel := context.WithTimeout(context.Background(), latencyProbeTimeout)
	defer cancel()

	res := <-ping.Ping(ctx, n.host, pid)
	if res.Error != nil {
		slog.Debug("latency probe failed", "peer", pid, "err", res.Error)
		return
	}
	n.scorer.RecordLatency(pid, res.RTT)
}

type localCapabilities struct {
	libraryRepo     app.LibraryRepository
	peerID          peer.ID
	canTranscode    bool
	uploadBandwidth int64
}

func (lc *localCapabilities) GetLocalCapabilities() *pb.PeerCapabilities {
	return &pb.PeerCapabilities{
		SupportedCodecs:   domain.PlayableCodecs,
		SupportedBitrates: domain.SupportedBitrates,
		ProtocolVersion:   domain.PeerProtocolVersion,
		CanTranscode:      lc.canTranscode,
		UploadBandwidth:   lc.uploadBandwidth,
	}
}

func (lc *localCapabilities) PeerID() peer.ID {
	return lc.peerID
}
