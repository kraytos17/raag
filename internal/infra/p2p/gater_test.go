package p2p

import (
	"net"
	"sync"
	"testing"

	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

var _ connmgr.ConnectionGater = (*PeerGater)(nil)

type mockConnMultiaddrs struct {
	remote ma.Multiaddr
	local  ma.Multiaddr
}

func (m *mockConnMultiaddrs) LocalMultiaddr() ma.Multiaddr  { return m.local }
func (m *mockConnMultiaddrs) RemoteMultiaddr() ma.Multiaddr { return m.remote }

func mustMultiaddr(t *testing.T, s string) ma.Multiaddr {
	t.Helper()
	maddr, err := ma.NewMultiaddr(s)
	if err != nil {
		t.Fatalf("bad multiaddr %q: %v", s, err)
	}
	return maddr
}

func TestPeerGater_BanUnbanPeer(t *testing.T) {
	g := NewPeerGater()
	pid := peer.ID("peer-1")

	if g.IsBanned(pid) {
		t.Fatal("new gater should not have banned peers")
	}

	g.Ban(pid)
	if !g.IsBanned(pid) {
		t.Fatal("IsBanned should be true after Ban()")
	}

	g.Unban(pid)
	if g.IsBanned(pid) {
		t.Fatal("IsBanned should be false after Unban()")
	}
}

func TestPeerGater_BanIdempotent(t *testing.T) {
	g := NewPeerGater()
	pid := peer.ID("peer-1")

	g.Ban(pid)
	g.Ban(pid) // double-ban should not panic
	if !g.IsBanned(pid) {
		t.Fatal("IsBanned should still be true")
	}
}

func TestPeerGater_UnbanNonExistent(t *testing.T) {
	g := NewPeerGater()
	g.Unban(peer.ID("never-banned")) // should not panic
}

func TestPeerGater_BanUnbanIP(t *testing.T) {
	g := NewPeerGater()

	if g.IsIPBanned("192.168.1.1") {
		t.Fatal("new gater should not have banned IPs")
	}

	g.BanIP("192.168.1.1")
	if !g.IsIPBanned("192.168.1.1") {
		t.Fatal("IsIPBanned should be true after BanIP()")
	}
	// Other IPs unaffected
	if g.IsIPBanned("10.0.0.1") {
		t.Fatal("10.0.0.1 should not be banned")
	}

	g.UnbanIP("192.168.1.1")
	if g.IsIPBanned("192.168.1.1") {
		t.Fatal("IsIPBanned should be false after UnbanIP()")
	}
}

func TestPeerGater_BanIPIdempotent(t *testing.T) {
	g := NewPeerGater()
	g.BanIP("10.0.0.1")
	g.BanIP("10.0.0.1")
	if !g.IsIPBanned("10.0.0.1") {
		t.Fatal("should still be banned")
	}
}

func TestPeerGater_InterceptPeerDial(t *testing.T) {
	g := NewPeerGater()
	pid := peer.ID("peer-1")

	if !g.InterceptPeerDial(pid) {
		t.Fatal("should allow dial to non-banned peer")
	}

	g.Ban(pid)
	if g.InterceptPeerDial(pid) {
		t.Fatal("should block dial to banned peer")
	}

	g.Unban(pid)
	if !g.InterceptPeerDial(pid) {
		t.Fatal("should allow dial after unban")
	}
}

func TestPeerGater_InterceptAddrDial(t *testing.T) {
	g := NewPeerGater()
	pid := peer.ID("peer-1")
	addr, _ := ma.NewMultiaddr("/ip4/192.168.1.1/tcp/4001")

	if !g.InterceptAddrDial(pid, addr) {
		t.Fatal("should allow addr dial to non-banned peer")
	}

	g.Ban(pid)
	if g.InterceptAddrDial(pid, addr) {
		t.Fatal("should block addr dial to banned peer")
	}
}

func TestPeerGater_InterceptAccept_Allowed(t *testing.T) {
	g := NewPeerGater()
	addrs := &mockConnMultiaddrs{
		remote: mustMultiaddr(t, "/ip4/192.168.1.50/tcp/4001"),
		local:  mustMultiaddr(t, "/ip4/0.0.0.0/tcp/7844"),
	}

	if !g.InterceptAccept(addrs) {
		t.Fatal("should accept connection from non-banned IP")
	}
}

func TestPeerGater_InterceptAccept_BannedIP(t *testing.T) {
	g := NewPeerGater()
	g.BanIP("192.168.1.50")

	addrs := &mockConnMultiaddrs{
		remote: mustMultiaddr(t, "/ip4/192.168.1.50/tcp/4001"),
		local:  mustMultiaddr(t, "/ip4/0.0.0.0/tcp/7844"),
	}

	if g.InterceptAccept(addrs) {
		t.Fatal("should reject connection from banned IP")
	}
}

func TestPeerGater_InterceptAccept_NilAddrs(t *testing.T) {
	g := NewPeerGater()
	// nil addrs should be safe and allowed
	if !g.InterceptAccept(nil) {
		t.Fatal("should allow when addrs is nil")
	}
}

func TestPeerGater_InterceptSecured_BannedPeer(t *testing.T) {
	g := NewPeerGater()
	pid := peer.ID("peer-evil")
	g.Ban(pid)

	addrs := &mockConnMultiaddrs{
		remote: mustMultiaddr(t, "/ip4/10.0.0.1/tcp/4001"),
	}

	if g.InterceptSecured(network.DirInbound, pid, addrs) {
		t.Fatal("should reject secured connection from banned peer")
	}
}

func TestPeerGater_InterceptSecured_BannedIP(t *testing.T) {
	g := NewPeerGater()
	pid := peer.ID("peer-ok")
	g.BanIP("10.0.0.1")

	addrs := &mockConnMultiaddrs{
		remote: mustMultiaddr(t, "/ip4/10.0.0.1/tcp/4001"),
	}

	if g.InterceptSecured(network.DirInbound, pid, addrs) {
		t.Fatal("should reject secured connection from banned IP")
	}
}

func TestPeerGater_InterceptSecured_Allowed(t *testing.T) {
	g := NewPeerGater()
	pid := peer.ID("peer-ok")

	addrs := &mockConnMultiaddrs{
		remote: mustMultiaddr(t, "/ip4/10.0.0.1/tcp/4001"),
	}

	if !g.InterceptSecured(network.DirInbound, pid, addrs) {
		t.Fatal("should allow secured connection for non-banned peer/IP")
	}
}

func TestPeerGater_InterceptSecured_BothDirections(t *testing.T) {
	g := NewPeerGater()
	pid := peer.ID("peer-ok")
	addrs := &mockConnMultiaddrs{
		remote: mustMultiaddr(t, "/ip4/10.0.0.2/tcp/4001"),
	}

	for _, dir := range []network.Direction{network.DirInbound, network.DirOutbound} {
		if !g.InterceptSecured(dir, pid, addrs) {
			t.Fatalf("should allow for direction %d", dir)
		}
	}
}

func TestPeerGater_InterceptUpgraded(t *testing.T) {
	g := NewPeerGater()
	// InterceptUpgraded always returns true
	ok, reason := g.InterceptUpgraded(nil)
	if !ok {
		t.Fatal("InterceptUpgraded should always return true")
	}
	if reason != 0 {
		t.Fatalf("InterceptUpgraded reason = %d, want 0", reason)
	}
}

func TestPeerGater_BlockPeer(t *testing.T) {
	g := NewPeerGater()
	pid := peer.ID("peer-1")

	if err := g.BlockPeer(pid); err != nil {
		t.Fatalf("BlockPeer() error = %v", err)
	}
	if !g.IsBanned(pid) {
		t.Fatal("BlockPeer should ban the peer")
	}
}

func TestPeerGater_UnblockPeer(t *testing.T) {
	g := NewPeerGater()
	pid := peer.ID("peer-1")
	g.Ban(pid)

	if err := g.UnblockPeer(pid); err != nil {
		t.Fatalf("UnblockPeer() error = %v", err)
	}
	if g.IsBanned(pid) {
		t.Fatal("UnblockPeer should unban the peer")
	}
}

func TestPeerGater_BlockAddr(t *testing.T) {
	g := NewPeerGater()
	ip := net.ParseIP("192.168.1.100")

	if err := g.BlockAddr(ip); err != nil {
		t.Fatalf("BlockAddr() error = %v", err)
	}
	if !g.IsIPBanned(ip.String()) {
		t.Fatal("BlockAddr should ban the IP")
	}
}

func TestPeerGater_UnblockAddr(t *testing.T) {
	g := NewPeerGater()
	ip := net.ParseIP("192.168.1.100")
	g.BanIP(ip.String())

	if err := g.UnblockAddr(ip); err != nil {
		t.Fatalf("UnblockAddr() error = %v", err)
	}
	if g.IsIPBanned(ip.String()) {
		t.Fatal("UnblockAddr should unban the IP")
	}
}

func TestExtractIPFromMultiaddrs_IPv4(t *testing.T) {
	addrs := &mockConnMultiaddrs{
		remote: mustMultiaddr(t, "/ip4/192.168.1.50/tcp/4001"),
		local:  mustMultiaddr(t, "/ip4/0.0.0.0/tcp/7844"),
	}

	ip := extractIPFromMultiaddrs(addrs)
	if ip != "192.168.1.50" {
		t.Fatalf("extractIPFromMultiaddrs() = %q, want %q", ip, "192.168.1.50")
	}
}

func TestExtractIPFromMultiaddrs_IPv6(t *testing.T) {
	addrs := &mockConnMultiaddrs{
		remote: mustMultiaddr(t, "/ip6/::1/tcp/4001"),
		local:  mustMultiaddr(t, "/ip4/0.0.0.0/tcp/7844"),
	}

	ip := extractIPFromMultiaddrs(addrs)
	if ip != "::1" {
		t.Fatalf("extractIPFromMultiaddrs() = %q, want %q", ip, "::1")
	}
}

func TestExtractIPFromMultiaddrs_Nil(t *testing.T) {
	ip := extractIPFromMultiaddrs(nil)
	if ip != "" {
		t.Fatalf("extractIPFromMultiaddrs(nil) = %q, want empty", ip)
	}
}

func TestExtractIPFromMultiaddrs_NilRemoteAndLocal(t *testing.T) {
	addrs := &mockConnMultiaddrs{
		remote: nil,
		local:  nil,
	}
	ip := extractIPFromMultiaddrs(addrs)
	if ip != "" {
		t.Fatalf("extractIPFromMultiaddrs(nil addrs) = %q, want empty", ip)
	}
}

func TestExtractIPFromMultiaddrs_FallbackToLocal(t *testing.T) {
	// remote has no IP component, should fall through to local
	addrs := &mockConnMultiaddrs{
		remote: nil,
		local:  mustMultiaddr(t, "/ip4/127.0.0.1/tcp/7844"),
	}

	ip := extractIPFromMultiaddrs(addrs)
	if ip != "127.0.0.1" {
		t.Fatalf("extractIPFromMultiaddrs() = %q, want %q", ip, "127.0.0.1")
	}
}

func TestPeerGater_ConcurrentBanUnban(t *testing.T) {
	g := NewPeerGater()
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			pid := peer.ID("peer")
			if n%2 == 0 {
				g.Ban(pid)
			} else {
				g.Unban(pid)
			}
			g.IsBanned(pid)
		}(i)
	}

	wg.Wait()
}

func TestPeerGater_ConcurrentIPBanUnban(t *testing.T) {
	g := NewPeerGater()
	var wg sync.WaitGroup

	for i := range 100 {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			ip := "10.0.0.1"
			if n%2 == 0 {
				g.BanIP(ip)
			} else {
				g.UnbanIP(ip)
			}
			g.IsIPBanned(ip)
		}(i)
	}

	wg.Wait()
}

func TestPeerGater_MultiplePeersIndependent(t *testing.T) {
	g := NewPeerGater()
	p1 := peer.ID("peer-1")
	p2 := peer.ID("peer-2")

	g.Ban(p1)

	if !g.IsBanned(p1) {
		t.Fatal("p1 should be banned")
	}
	if g.IsBanned(p2) {
		t.Fatal("p2 should not be banned")
	}

	if !g.InterceptPeerDial(p2) {
		t.Fatal("dial to p2 should be allowed")
	}
	if g.InterceptPeerDial(p1) {
		t.Fatal("dial to p1 should be blocked")
	}
}

func TestPeerGater_MultipleIPsIndependent(t *testing.T) {
	g := NewPeerGater()

	g.BanIP("10.0.0.1")

	if !g.IsIPBanned("10.0.0.1") {
		t.Fatal("10.0.0.1 should be banned")
	}
	if g.IsIPBanned("10.0.0.2") {
		t.Fatal("10.0.0.2 should not be banned")
	}
}

func TestPeerGater_LANOnly_InterceptAddrDial(t *testing.T) {
	g := NewPeerGater()
	g.SetLANOnly(true)

	pid := peer.ID("peer")

	tests := []struct {
		name    string
		addr    string
		allowed bool
	}{
		{name: "public ipv4 rejected", addr: "/ip4/8.8.8.8/tcp/4001", allowed: false},
		{name: "private ipv4 allowed", addr: "/ip4/192.168.1.5/tcp/4001", allowed: true},
		{name: "rfc1918 10/8 allowed", addr: "/ip4/10.0.0.2/tcp/4001", allowed: true},
		{name: "link-local allowed", addr: "/ip4/169.254.10.10/tcp/4001", allowed: true},
		{name: "loopback allowed", addr: "/ip4/127.0.0.1/tcp/4001", allowed: true},
		{name: "public ipv6 rejected", addr: "/ip6/2606:4700::1111/tcp/4001", allowed: false},
		{name: "ula ipv6 allowed", addr: "/ip6/fd00::1/tcp/4001", allowed: true},
		{name: "link-local ipv6 allowed", addr: "/ip6/fe80::1/tcp/4001", allowed: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := mustMultiaddr(t, tt.addr)
			if got := g.InterceptAddrDial(pid, m); got != tt.allowed {
				t.Errorf("InterceptAddrDial(%s) = %v, want %v", tt.addr, got, tt.allowed)
			}
		})
	}
}

func TestPeerGater_LANOnly_InterceptAccept(t *testing.T) {
	g := NewPeerGater()
	g.SetLANOnly(true)

	private := &mockConnMultiaddrs{remote: mustMultiaddr(t, "/ip4/192.168.1.10/tcp/4001")}
	public := &mockConnMultiaddrs{remote: mustMultiaddr(t, "/ip4/8.8.8.8/tcp/4001")}

	if !g.InterceptAccept(private) {
		t.Error("InterceptAccept(private) should be allowed")
	}
	if g.InterceptAccept(public) {
		t.Error("InterceptAccept(public) should be rejected")
	}
}

func TestPeerGater_LANOnly_InterceptSecured(t *testing.T) {
	g := NewPeerGater()
	g.SetLANOnly(true)

	pid := peer.ID("peer")
	private := &mockConnMultiaddrs{remote: mustMultiaddr(t, "/ip4/192.168.1.10/tcp/4001")}
	public := &mockConnMultiaddrs{remote: mustMultiaddr(t, "/ip4/8.8.8.8/tcp/4001")}

	if !g.InterceptSecured(network.DirInbound, pid, private) {
		t.Error("InterceptSecured(private) should be allowed")
	}
	if g.InterceptSecured(network.DirInbound, pid, public) {
		t.Error("InterceptSecured(public) should be rejected")
	}
}

func TestPeerGater_NoLANOnly_AllowsPublic(t *testing.T) {
	g := NewPeerGater() // lanOnly defaults false

	pid := peer.ID("peer")
	public := mustMultiaddr(t, "/ip4/8.8.8.8/tcp/4001")
	addrs := &mockConnMultiaddrs{remote: public}

	if !g.InterceptAddrDial(pid, public) {
		t.Error("InterceptAddrDial(public) should be allowed when not LAN-only")
	}
	if !g.InterceptAccept(addrs) {
		t.Error("InterceptAccept(public) should be allowed when not LAN-only")
	}
	if !g.InterceptSecured(network.DirInbound, pid, addrs) {
		t.Error("InterceptSecured(public) should be allowed when not LAN-only")
	}
}

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"169.254.1.1", true},
		{"127.0.0.1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"fd00::1", true},
		{"fe80::1", true},
		{"::1", true},
		{"2606:4700::1111", false},
		{"", false},
		{"not-an-ip", false},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := isPrivateIP(tt.ip); got != tt.want {
				t.Errorf("isPrivateIP(%q) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}
