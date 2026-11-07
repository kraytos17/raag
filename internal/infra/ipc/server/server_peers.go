package server

import (
	"context"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/convert"
	pb "github.com/p-society/raag/proto/gen"
)

func (s *Server) handleListPeers(ctx context.Context) *pb.Response {
	if s.peerRepo == nil {
		return &pb.Response{
			Success: false,
			Error:   errP2PNotEnabled,
		}
	}

	var pbPeers []*pb.Peer
	seen := make(map[string]struct{})
	for p, err := range s.peerRepo.ListAll(ctx) {
		if err != nil {
			continue
		}
		seen[p.ID.String()] = struct{}{}
		if s.p2pNode != nil {
			if enriched := s.p2pNode.PeerStatus(p); enriched != nil {
				pbPeers = append(pbPeers, enriched)
				continue
			}
		}
		pbPeers = append(pbPeers, convert.PeerInfoToProto(p))
	}

	// Surface live connected peers that aren't yet persisted (e.g. a
	// brand-new broadcast discovery), so `raag peers list` is never empty
	// right after a connect — `network` already shows them, `peers list`
	// should be consistent.
	if s.p2pNode != nil {
		connected := s.p2pNode.Peers()
		connectedSet := make(map[peer.ID]struct{}, len(connected))
		for _, pid := range connected {
			connectedSet[pid] = struct{}{}
			if _, ok := seen[pid.String()]; ok {
				continue
			}

			pi := s.p2pNode.Host().Peerstore().PeerInfo(pid)
			addrs := make([]string, len(pi.Addrs))
			for i, a := range pi.Addrs {
				addrs[i] = a.String()
			}

			pbPeers = append(pbPeers, &pb.Peer{Id: pid.String(), Addrs: addrs, Connected: true})
			seen[pid.String()] = struct{}{}
		}
		for _, pi := range s.p2pNode.DiscoveredPeers() {
			if _, ok := seen[pi.ID.String()]; ok {
				continue
			}
			if _, ok := connectedSet[pi.ID]; ok {
				continue
			}

			addrs := make([]string, len(pi.Addrs))
			for i, a := range pi.Addrs {
				addrs[i] = a.String()
			}
			pbPeers = append(pbPeers, &pb.Peer{Id: pi.ID.String(), Addrs: addrs, Connected: false})
			seen[pi.ID.String()] = struct{}{}
		}
	}
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_ListPeers{ListPeers: &pb.ListPeersResponse{Peers: pbPeers}},
	}
}

func (s *Server) handleNetworkStatus() *pb.Response {
	if s.p2pNode == nil {
		return &pb.Response{
			Success: false,
			Error:   errP2PNotEnabled,
		}
	}
	info := s.p2pNode.NetworkInfo()

	var connectedPeers []*pb.ConnectedPeer
	for _, p := range info.ConnectedPeers {
		var addrs []string
		for _, a := range p.Addrs {
			addrs = append(addrs, a.String())
		}
		connectedPeers = append(connectedPeers, &pb.ConnectedPeer{
			PeerId:   p.ID.String(),
			Addrs:    addrs,
			Dialable: true,
		})
	}

	var discoveredPeers []*pb.ConnectedPeer
	for _, p := range info.DiscoveredPeers {
		ps := s.p2pNode.Host().Peerstore().PeerInfo(p.ID)
		dialable := len(ps.Addrs) > 0

		var maddrs []string
		if dialable {
			for _, a := range ps.Addrs {
				maddrs = append(maddrs, a.String())
			}
		} else {
			for _, a := range p.Addrs {
				maddrs = append(maddrs, a.String())
			}
		}

		discoveredPeers = append(discoveredPeers, &pb.ConnectedPeer{
			PeerId:   p.ID.String(),
			Addrs:    maddrs,
			Dialable: dialable,
		})
	}

	return &pb.Response{
		Success: true,
		Payload: &pb.Response_NetworkStatus{NetworkStatus: &pb.NetworkStatusResponse{
			PeerId:          info.PeerID,
			ListenAddrs:     info.ListenAddrs,
			ConnectedPeers:  connectedPeers,
			DiscoveredPeers: discoveredPeers,
		}},
	}
}

func (s *Server) handleBanPeer(req *pb.BanPeerRequest) *pb.Response {
	if s.p2pNode == nil {
		return &pb.Response{
			Success: false,
			Error:   errP2PNotEnabled,
		}
	}
	s.p2pNode.BanPeer(peer.ID(req.PeerId))
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_BanPeer{},
	}
}

func (s *Server) handleUnbanPeer(req *pb.UnbanPeerRequest) *pb.Response {
	if s.p2pNode == nil {
		return &pb.Response{
			Success: false,
			Error:   errP2PNotEnabled,
		}
	}
	s.p2pNode.UnbanPeer(peer.ID(req.PeerId))
	return &pb.Response{
		Success: true,
		Payload: &pb.Response_UnbanPeer{},
	}
}
