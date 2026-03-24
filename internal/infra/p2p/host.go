package p2p

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	ma "github.com/multiformats/go-multiaddr"
)

type P2PConfig struct {
	ListenAddrs     []string
	AnnounceAddrs   []string
	BootstrapPeers  []string
	MdnsServiceName string

	ConnMgrLowMark  int
	ConnMgrHighMark int
	ConnMgrGrace    time.Duration
}

func DefaultP2PConfig() P2PConfig {
	return P2PConfig{
		ListenAddrs:     []string{"/ip4/0.0.0.0/tcp/7844"},
		AnnounceAddrs:   nil,
		BootstrapPeers:  nil,
		MdnsServiceName: "raag-local",
		ConnMgrLowMark:  32,
		ConnMgrHighMark: 64,
		ConnMgrGrace:    30 * time.Second,
	}
}

func NewHost(privKey crypto.PrivKey, cfg P2PConfig) (host.Host, error) {
	resourceManager, err := rcmgr.NewResourceManager(
		rcmgr.NewFixedLimiter(rcmgr.DefaultLimits.Scale(
			4<<20, // 4MiB memory base
			1024,  // 64 peers * 3 streams + overhead
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource manager: %w", err)
	}

	connManager, err := connmgr.NewConnManager(
		cfg.ConnMgrLowMark,
		cfg.ConnMgrHighMark,
		connmgr.WithGracePeriod(cfg.ConnMgrGrace),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create conn manager: %w", err)
	}

	var listenAddrs []ma.Multiaddr
	for _, addr := range cfg.ListenAddrs {
		maddr, err := ma.NewMultiaddr(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid listen addr %s: %w", addr, err)
		}
		listenAddrs = append(listenAddrs, maddr)
	}

	opts := []libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.ResourceManager(resourceManager),
		libp2p.ConnectionManager(connManager),
		libp2p.NATPortMap(),
		libp2p.Ping(true),
	}

	if len(cfg.AnnounceAddrs) > 0 {
		var announceAddrs []ma.Multiaddr
		for _, addr := range cfg.AnnounceAddrs {
			maddr, err := ma.NewMultiaddr(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid announce addr %s: %w", addr, err)
			}
			announceAddrs = append(announceAddrs, maddr)
		}
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

	var wg sync.WaitGroup
	for _, pi := range addrInfos {
		wg.Add(1)
		go func(addr peer.AddrInfo) {
			defer wg.Done()
			connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := h.Connect(connectCtx, addr); err != nil {
				slog.Debug("bootstrap: failed to connect", "peer", addr.ID, "err", err)
			}
		}(pi)
	}
	wg.Wait()
	return nil
}
