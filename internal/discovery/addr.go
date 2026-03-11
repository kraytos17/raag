package discovery

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

func AddrInfoStrings(peers []peer.AddrInfo) []string {
	var multiaddrs []string
	for _, peerInfo := range peers {
		for _, addr := range peerInfo.Addrs {
			fullAddr := addr.Encapsulate(multiaddr.StringCast("/p2p/" + peerInfo.ID.String()))
			multiaddrs = append(multiaddrs, fullAddr.String())
		}
	}
	return multiaddrs
}
