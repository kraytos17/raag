package p2p

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

var _ connmgr.ConnectionGater = (*PeerGater)(nil)

var (
	ErrPeerBanned = errors.New("peer is banned")
	ErrIPBanned   = errors.New("IP address is banned")
)

type PeerGater struct {
	bannedPeers sync.Map
	bannedIPs   sync.Map
	lanOnly     atomic.Bool
}

func NewPeerGater() *PeerGater {
	return &PeerGater{}
}

// SetLANOnly toggles private-IP-only enforcement: when enabled, inbound and
// outbound connections are restricted to private, link-local, and loopback
// addresses (RFC1918, ULA, 169.254/16, fe80::/10, 127/8, ::1).
func (g *PeerGater) SetLANOnly(enabled bool) {
	g.lanOnly.Store(enabled)
}

func (g *PeerGater) isLANOnly() bool {
	return g.lanOnly.Load()
}

func (g *PeerGater) Ban(pid peer.ID) {
	g.bannedPeers.Store(pid, true)
}

func (g *PeerGater) Unban(pid peer.ID) {
	g.bannedPeers.Delete(pid)
}

func (g *PeerGater) IsBanned(pid peer.ID) bool {
	if banned, ok := g.bannedPeers.Load(pid); ok && banned.(bool) {
		return true
	}
	return false
}

func (g *PeerGater) BanIP(ip string) {
	g.bannedIPs.Store(ip, true)
}

func (g *PeerGater) UnbanIP(ip string) {
	g.bannedIPs.Delete(ip)
}

func (g *PeerGater) IsIPBanned(ip string) bool {
	if banned, ok := g.bannedIPs.Load(ip); ok && banned.(bool) {
		return true
	}
	return false
}

func (g *PeerGater) InterceptPeerDial(p peer.ID) bool {
	return !g.IsBanned(p)
}

func (g *PeerGater) InterceptAddrDial(p peer.ID, m ma.Multiaddr) bool {
	if g.IsBanned(p) {
		return false
	}
	if g.isLANOnly() && !isPrivateMultiaddr(m) {
		return false
	}
	return true
}

func (g *PeerGater) InterceptAccept(addrs network.ConnMultiaddrs) bool {
	ip := extractIPFromMultiaddrs(addrs)
	if ip != "" && g.IsIPBanned(ip) {
		return false
	}
	if g.isLANOnly() && !isPrivateIP(ip) {
		return false
	}
	return true
}

func (g *PeerGater) InterceptSecured(direction network.Direction, p peer.ID, addrs network.ConnMultiaddrs) bool {
	if g.IsBanned(p) {
		return false
	}

	ip := extractIPFromMultiaddrs(addrs)
	if ip != "" && g.IsIPBanned(ip) {
		return false
	}
	if g.isLANOnly() && !isPrivateIP(ip) {
		return false
	}
	return true
}

func (g *PeerGater) InterceptUpgraded(conn network.Conn) (bool, control.DisconnectReason) {
	return true, 0
}

func extractIPFromMultiaddrs(addrs network.ConnMultiaddrs) string {
	if addrs == nil {
		return ""
	}

	maddrs := []ma.Multiaddr{addrs.RemoteMultiaddr(), addrs.LocalMultiaddr()}
	for _, maddr := range maddrs {
		if maddr == nil {
			continue
		}
		if ip, err := maddr.ValueForProtocol(ma.P_IP4); err == nil {
			return ip
		}
		if ip, err := maddr.ValueForProtocol(ma.P_IP6); err == nil {
			return ip
		}
	}

	return ""
}

// isPrivateMultiaddr reports whether the address contains only private,
// link-local, or loopback IPs. Used to enforce LAN-only mode.
func isPrivateMultiaddr(m ma.Multiaddr) bool {
	if m == nil {
		return false
	}
	if ip, err := m.ValueForProtocol(ma.P_IP4); err == nil {
		return isPrivateIP(ip)
	}
	if ip, err := m.ValueForProtocol(ma.P_IP6); err == nil {
		return isPrivateIP(ip)
	}
	return false
}

// isPrivateIP reports whether ip is in a private, link-local, or loopback
// range: RFC1918 (10/8, 172.16/12, 192.168/16), ULA (fc00::/7),
// link-local (169.254/16, fe80::/10), and loopback (127/8, ::1). An empty or
// unparseable value is treated as non-private so LAN-only mode stays strict.
func isPrivateIP(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	return parsed.IsPrivate() || parsed.IsLinkLocalUnicast() || parsed.IsLoopback()
}

func (g *PeerGater) BlockPeer(pid peer.ID) error {
	g.Ban(pid)
	return nil
}

func (g *PeerGater) UnblockPeer(pid peer.ID) error {
	g.Unban(pid)
	return nil
}

func (g *PeerGater) BlockAddr(ip net.IP) error {
	g.BanIP(ip.String())
	return nil
}

func (g *PeerGater) UnblockAddr(ip net.IP) error {
	g.UnbanIP(ip.String())
	return nil
}
