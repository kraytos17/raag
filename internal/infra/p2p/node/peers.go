package node

import (
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/convert"
	"github.com/p-society/raag/internal/domain"
	pb "github.com/p-society/raag/proto/gen"
)

func (n *Node) Peers() []peer.ID {
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
func (n *Node) DiscoveredPeers() []peer.AddrInfo {
	return n.mdnsDiscovered.All()
}

// AddDiscovered records a discovered peer (e.g. from mDNS) so it is visible via
// DiscoveredPeers/ListPeers.
func (n *Node) AddDiscovered(pi peer.AddrInfo) {
	n.mdnsDiscovered.Add(pi)
}

// PeerStatus enriches a peer with live connection state and capabilities.
// The peer's score (if any) is preserved from the supplied info.
func (n *Node) PeerStatus(info *domain.PeerInfo) *pb.Peer {
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
func (n *Node) overlayLiveScore(peer *pb.Peer, pid peer.ID) {
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

func (n *Node) NetworkInfo() NetworkInfo {
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
