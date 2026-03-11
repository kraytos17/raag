package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

type PeerPersistence struct {
	peersFile string
}

func NewPeerPersistence() *PeerPersistence {
	configDir, _ := os.UserConfigDir()
	peersFile := filepath.Join(configDir, "raag", "peers.json")
	return &PeerPersistence{peersFile: peersFile}
}

func (p *PeerPersistence) Load() ([]peer.AddrInfo, error) {
	data, err := os.ReadFile(p.peersFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var multiaddrs []string
	if err := json.Unmarshal(data, &multiaddrs); err != nil {
		return nil, err
	}

	var peers []peer.AddrInfo
	for _, addrStr := range multiaddrs {
		ma, err := multiaddr.NewMultiaddr(addrStr)
		if err != nil {
			continue
		}
		addrInfo, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			continue
		}
		peers = append(peers, *addrInfo)
	}
	return peers, nil
}

func (p *PeerPersistence) Save(peers []peer.AddrInfo) error {
	var multiaddrs []string
	for _, peerInfo := range peers {
		for _, addr := range peerInfo.Addrs {
			fullAddr := addr.Encapsulate(multiaddr.StringCast("/p2p/" + peerInfo.ID.String()))
			multiaddrs = append(multiaddrs, fullAddr.String())
		}
	}

	data, err := json.MarshalIndent(multiaddrs, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(p.peersFile)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(p.peersFile, data, 0o644)
}
