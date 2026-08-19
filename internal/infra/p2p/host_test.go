package p2p

import (
	"errors"
	"net"
	"strconv"
	"testing"

	ma "github.com/multiformats/go-multiaddr"
)

func TestProbeListenAddrs_FreePorts(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tcpPort := tcp.Addr().(*net.TCPAddr).Port
	_ = tcp.Close()

	udp, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udpPort := udp.LocalAddr().(*net.UDPAddr).Port
	_ = udp.Close()

	addrs := []ma.Multiaddr{
		ma.StringCast("/ip4/127.0.0.1/tcp/" + strconv.Itoa(tcpPort)),
		ma.StringCast("/ip4/127.0.0.1/udp/" + strconv.Itoa(udpPort) + "/quic-v1"),
	}
	if err := probeListenAddrs(addrs); err != nil {
		t.Fatalf("probeListenAddrs(free ports) error = %v, want nil", err)
	}
}

func TestProbeListenAddrs_TCPInUse(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port := l.Addr().(*net.TCPAddr).Port

	addrs := []ma.Multiaddr{ma.StringCast("/ip4/127.0.0.1/tcp/" + strconv.Itoa(port))}
	if err := probeListenAddrs(addrs); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("probeListenAddrs(tcp in use) error = %v, want ErrPortInUse", err)
	}
}

func TestProbeListenAddrs_UDPInUse(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port

	addrs := []ma.Multiaddr{ma.StringCast("/ip4/127.0.0.1/udp/" + strconv.Itoa(port) + "/quic-v1")}
	if err := probeListenAddrs(addrs); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("probeListenAddrs(udp in use) error = %v, want ErrPortInUse", err)
	}
}

func TestProbeListenAddrs_SkipsPortZero(t *testing.T) {
	addrs := []ma.Multiaddr{
		ma.StringCast("/ip4/0.0.0.0/tcp/0"),
		ma.StringCast("/ip4/0.0.0.0/udp/0/quic-v1"),
	}
	if err := probeListenAddrs(addrs); err != nil {
		t.Fatalf("probeListenAddrs(port 0) error = %v, want nil (ephemeral ports never conflict)", err)
	}
}
