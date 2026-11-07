package node

import (
	"context"
	"errors"
	"log/slog"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/domain"
	p2p "github.com/p-society/raag/internal/infra/p2p"
	"github.com/p-society/raag/internal/infra/p2p/discovery"
	protocols "github.com/p-society/raag/internal/infra/p2p/protocols"
)

func (n *Node) Start(ctx context.Context, bus domain.EventBus) error {
	n.lifecycleMu.Lock()
	defer n.lifecycleMu.Unlock()

	if n.stopped {
		return ErrNodeStopped
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
		peerConnections: make(map[peer.ID]int),
	}
	for _, pid := range n.host.Network().Peers() {
		notifier.peerConnections[pid] = len(n.host.Network().ConnsToPeer(pid))
	}

	n.notifier = notifier
	n.host.Network().Notify(n.notifier)
	for _, conn := range n.host.Network().Conns() {
		pid := conn.RemotePeer()
		if n.peerMgr.IsBanned(pid) {
			conn.Close()
			continue
		}

		pi := n.host.Peerstore().PeerInfo(pid)
		n.peerMgr.CachePeer(pi)
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
			if err := p2p.BootstrapPeers(n.ctx, n.host, n.bootstrapPeers); err != nil {
				slog.Warn("bootstrap failed", "err", err)
			}
		})
	}

	n.started = true
	slog.Info("P2P node started", "peer_id", n.host.ID())
	return nil
}

func (n *Node) failStart(ctx context.Context, startErr error) error {
	if stopErr := n.stopLocked(ctx); stopErr != nil {
		return errors.Join(startErr, stopErr)
	}
	return startErr
}

func (n *Node) Stop(ctx context.Context) error {
	n.lifecycleMu.Lock()
	defer n.lifecycleMu.Unlock()
	return n.stopLocked(ctx)
}

// stopLocked is shared by normal shutdown and startup rollback. The caller
// must hold lifecycleMu.
func (n *Node) stopLocked(ctx context.Context) error {
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
func (n *Node) startBackground(work func()) bool {
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
