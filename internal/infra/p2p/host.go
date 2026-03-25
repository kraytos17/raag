package p2p

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
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

func listenPorts(listenAddrs []ma.Multiaddr) (tcpPorts []int, quicPorts []int, err error) {
	seenTCP := map[int]bool{}
	seenQUIC := map[int]bool{}
	for _, a := range listenAddrs {
		if tcpStr, err := a.ValueForProtocol(ma.P_TCP); err == nil {
			port, err := strconv.Atoi(tcpStr)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid tcp port %q: %w", tcpStr, err)
			}
			if !seenTCP[port] {
				seenTCP[port] = true
				tcpPorts = append(tcpPorts, port)
			}
		}
		if _, err := a.ValueForProtocol(ma.P_QUIC_V1); err == nil {
			udpStr, err := a.ValueForProtocol(ma.P_UDP)
			if err != nil {
				return nil, nil, errors.New("quic-v1 listen addr missing udp port")
			}

			port, err := strconv.Atoi(udpStr)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid udp port %q: %w", udpStr, err)
			}
			if !seenQUIC[port] {
				seenQUIC[port] = true
				quicPorts = append(quicPorts, port)
			}
		}
	}
	return tcpPorts, quicPorts, nil
}

type P2PConfig struct {
	ListenAddrs     []string
	AnnounceAddrs   []string
	BootstrapPeers  []string
	MdnsServiceName string

	ConnMgrLowMark  int
	ConnMgrHighMark int
	ConnMgrGrace    time.Duration

	ConnectionGater *PeerGater
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

	_, _, err = listenPorts(listenAddrs)
	if err != nil {
		return nil, err
	}

	opts := []libp2p.Option{
		libp2p.Identity(privKey),
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.ResourceManager(resourceManager),
		libp2p.ConnectionManager(connManager),
		libp2p.Ping(true),
		libp2p.ForceReachabilityPrivate(),
	}

	if cfg.ConnectionGater != nil {
		opts = append(opts, libp2p.ConnectionGater(cfg.ConnectionGater))
	}
	if len(cfg.AnnounceAddrs) > 0 {
		var maddrs []ma.Multiaddr
		for _, addr := range cfg.AnnounceAddrs {
			maddr, err := ma.NewMultiaddr(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid announce addr %s: %w", addr, err)
			}
			maddrs = append(maddrs, maddr)
		}

		opts = append(opts, libp2p.AddrsFactory(func(addrs []ma.Multiaddr) []ma.Multiaddr {
			seen := make(map[string]struct{}, len(addrs)+len(maddrs))
			out := make([]ma.Multiaddr, 0, len(addrs)+len(maddrs))
			for _, a := range addrs {
				k := a.String()
				if _, ok := seen[k]; ok {
					continue
				}

				seen[k] = struct{}{}
				out = append(out, a)
			}
			for _, a := range maddrs {
				k := a.String()
				if _, ok := seen[k]; ok {
					continue
				}

				seen[k] = struct{}{}
				out = append(out, a)
			}
			return out
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
