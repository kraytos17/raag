package node

import (
	"context"
	"log/slog"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	"github.com/p-society/raag/internal/domain"
)

func (n *Node) measureLatencyLoop() {
	runTickerLoop(n.ctx, 30*time.Second, n.measureAllPeersLatency)
}

// manifestRefreshInterval is how often peer library manifests are re-fetched so
// library changes propagate well under the 24h PeerDataTTL.
const manifestRefreshInterval = 5 * time.Minute

// refreshManifestsLoop periodically re-fetches manifests from connected peers
// so their libraries stay fresh. FetchPeerData is idempotent and cancels any
// prior in-flight fetch per peer, so overlapping ticks are safe.
func (n *Node) refreshManifestsLoop() {
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
func (n *Node) refreshAllManifests() {
	ctx := n.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	for _, pid := range n.Peers() {
		n.peerMgr.FetchPeerData(ctx, pid)
	}
}

func (n *Node) measureAllPeersLatency() {
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
func (n *Node) probePeerLatency(pid peer.ID) {
	ctx, cancel := context.WithTimeout(context.Background(), latencyProbeTimeout)
	defer cancel()

	res := <-ping.Ping(ctx, n.host, pid)
	if res.Error != nil {
		slog.Debug("latency probe failed", "peer", pid, "err", res.Error)
		return
	}
	n.scorer.RecordLatency(pid, res.RTT)
}
