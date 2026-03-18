package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ds "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"

	"github.com/p-society/raag/internal/logger"
)

type PeerStore struct {
	ds ds.Batching
}

func NewPeerStore(ds ds.Batching) *PeerStore {
	return &PeerStore{ds: ds}
}

func (s *PeerStore) SavePeer(ctx context.Context, info peer.AddrInfo) error {
	if info.ID == "" {
		return fmt.Errorf("cannot save peer with empty ID")
	}
	if len(info.Addrs) == 0 {
		return nil
	}

	addrStrs := make([]string, 0, len(info.Addrs))
	for _, a := range info.Addrs {
		addrStrs = append(addrStrs, a.String())
	}

	data, err := json.Marshal(addrStrs)
	if err != nil {
		return fmt.Errorf("marshal peer addrs for %s: %w", info.ID, err)
	}

	key := peerKey(info.ID)
	return s.ds.Put(ctx, key, data)
}

func (s *PeerStore) SavePeers(ctx context.Context, peers []peer.AddrInfo) error {
	for _, info := range peers {
		if err := s.SavePeer(ctx, info); err != nil {
			logger.Debugf("SavePeers: failed to save %s: %v", info.ID, err)
		}
	}
	return nil
}

func (s *PeerStore) LoadPeers(ctx context.Context) ([]peer.AddrInfo, error) {
	q := dsq.Query{Prefix: "/peers/"}
	results, err := s.ds.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("query peers: %w", err)
	}
	defer results.Close()

	var out []peer.AddrInfo
	for r := range results.Next() {
		if r.Error != nil {
			logger.Debugf("LoadPeers: query error (skipping entry): %v", r.Error)
			continue
		}

		pidStr := strings.TrimPrefix(r.Key, "/peers/")
		if pidStr == "" {
			continue
		}

		pid, err := peer.Decode(pidStr)
		if err != nil {
			logger.Debugf("LoadPeers: invalid peer ID key %q (skipping): %v", pidStr, err)
			continue
		}

		var addrStrs []string
		if err := json.Unmarshal(r.Value, &addrStrs); err != nil {
			logger.Debugf("LoadPeers: bad JSON for peer %s (skipping): %v", pid, err)
			continue
		}

		var addrs []multiaddr.Multiaddr
		for _, addrStr := range addrStrs {
			ma, err := multiaddr.NewMultiaddr(addrStr)
			if err != nil {
				logger.Debugf("LoadPeers: bad multiaddr %q for peer %s (skipping addr): %v",
					addrStr, pid, err)
				continue
			}
			addrs = append(addrs, ma)
		}

		if len(addrs) == 0 {
			continue
		}

		out = append(out, peer.AddrInfo{ID: pid, Addrs: addrs})
	}

	logger.Debugf("LoadPeers: loaded %d peers from persistent store", len(out))
	return out, nil
}

func (s *PeerStore) RemovePeer(ctx context.Context, id peer.ID) error {
	return s.ds.Delete(ctx, peerKey(id))
}

func peerKey(id peer.ID) ds.Key {
	return ds.NewKey("/peers/" + id.String())
}
