package p2p

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

type P2PConfig struct {
	ListenAddrs     []string
	AnnounceAddrs   []string
	BootstrapPeers  []string
	MdnsServiceName string
}

func DefaultP2PConfig() P2PConfig {
	return P2PConfig{
		ListenAddrs:     []string{"/ip4/0.0.0.0/tcp/7844"},
		AnnounceAddrs:   nil,
		BootstrapPeers:  nil,
		MdnsServiceName: "raag-local",
	}
}

func NewHost(privKey crypto.PrivKey, cfg P2PConfig) (host.Host, error) {
	var listenAddrs []ma.Multiaddr
	for _, addr := range cfg.ListenAddrs {
		maddr, err := ma.NewMultiaddr(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid listen addr %s: %w", addr, err)
		}
		listenAddrs = append(listenAddrs, maddr)
	}

	var announceAddrs []ma.Multiaddr
	for _, addr := range cfg.AnnounceAddrs {
		maddr, err := ma.NewMultiaddr(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid announce addr %s: %w", addr, err)
		}
		announceAddrs = append(announceAddrs, maddr)
	}

	opts := []libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.DisableRelay(),
	}
	if len(announceAddrs) > 0 {
		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			return append(addrs, announceAddrs...)
		}))
	}

	return libp2p.New(opts...)
}

func BootstrapPeers(ctx context.Context, h host.Host, peers []string) error {
	if len(peers) == 0 {
		return nil
	}

	var addrInfos []peer.AddrInfo
	for _, peerStr := range peers {
		pi, err := peer.AddrInfoFromString(peerStr)
		if err != nil {
			slog.Debug("bootstrap: skipping invalid addr", "addr", peerStr, "err", err)
			continue
		}
		addrInfos = append(addrInfos, *pi)
	}
	if len(addrInfos) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	for _, pi := range addrInfos {
		if err := h.Connect(ctx, pi); err != nil {
			slog.Debug("bootstrap: failed to connect", "peer", pi.ID, "err", err)
		}
	}
	return nil
}
