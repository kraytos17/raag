package storage

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"

	ds "github.com/ipfs/go-datastore"
	dsq "github.com/ipfs/go-datastore/query"
	"github.com/libp2p/go-libp2p/core/peer"
)

type PeerStore struct {
	ds ds.Batching
}

type peerAddrEntry struct {
	AddrInfo peer.AddrInfo
}

func init() {
	gob.Register(peer.AddrInfo{})
}

func NewPeerStore(ds ds.Batching) *PeerStore {
	return &PeerStore{ds: ds}
}

func (s *PeerStore) SavePeer(ctx context.Context, info peer.AddrInfo) error {
	if info.ID == "" {
		return fmt.Errorf("cannot save peer with empty ID")
	}

	data, err := encodeAddrInfo(info)
	if err != nil {
		return fmt.Errorf("failed to encode peer address: %w", err)
	}

	key := peerKey(info.ID)
	return s.ds.Put(ctx, key, data)
}

func (s *PeerStore) SavePeers(ctx context.Context, peers []peer.AddrInfo) error {
	for _, info := range peers {
		if err := s.SavePeer(ctx, info); err != nil {
			return err
		}
	}
	return nil
}

func (s *PeerStore) LoadPeers(ctx context.Context) ([]peer.AddrInfo, error) {
	q := dsq.Query{Prefix: "/peers/"}
	results, err := s.ds.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failed to query peers: %w", err)
	}
	defer results.Close()

	var result []peer.AddrInfo
	for r := range results.Next() {
		if r.Error != nil {
			return result, r.Error
		}
		info, err := decodeAddrInfo(r.Value)
		if err != nil {
			continue
		}
		result = append(result, info)
	}
	return result, nil
}

func (s *PeerStore) LoadPeer(ctx context.Context, id peer.ID) (peer.AddrInfo, error) {
	key := peerKey(id)
	data, err := s.ds.Get(ctx, key)
	if err != nil {
		return peer.AddrInfo{}, fmt.Errorf("failed to load peer: %w", err)
	}
	return decodeAddrInfo(data)
}

func (s *PeerStore) RemovePeer(ctx context.Context, id peer.ID) error {
	key := peerKey(id)
	return s.ds.Delete(ctx, key)
}

func peerKey(id peer.ID) ds.Key {
	return ds.NewKey("/peers/" + string(id))
}

func encodeAddrInfo(info peer.AddrInfo) ([]byte, error) {
	entry := peerAddrEntry{AddrInfo: info}
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	if err := enc.Encode(entry); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func decodeAddrInfo(data []byte) (peer.AddrInfo, error) {
	var entry peerAddrEntry
	dec := gob.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&entry); err != nil {
		return peer.AddrInfo{}, err
	}
	return entry.AddrInfo, nil
}
