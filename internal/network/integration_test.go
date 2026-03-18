package network

import (
	"context"
	"crypto/rand"
	"net"
	"strings"
	"testing"
	"time"

	crypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	appconfig "github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/library"
	"github.com/spf13/viper"
)

func TestNetworkManagerCreation(t *testing.T) {
	manager, cleanup := newTestNetworkManager(t, "")
	defer cleanup()

	if manager.Host() == nil {
		t.Fatal("host should be set")
	}
}

func TestPeerConnection(t *testing.T) {
	managerA, cleanupA := newTestNetworkManager(t, "")
	defer cleanupA()

	managerB, cleanupB := newTestNetworkManager(t, "")
	defer cleanupB()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	peerInfo := peer.AddrInfo{
		ID:    managerB.Host().ID(),
		Addrs: managerB.Host().Addrs(),
	}

	if err := managerA.Connect(ctx, peerInfo); err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	if !managerA.IsOnline() {
		t.Fatal("managerA should be online after connection")
	}
}

func TestNetworkHostExposesTCPAndQUICAddresses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	manager, cleanup := newTestNetworkManager(t, "")
	defer cleanup()

	addrs := manager.Host().Addrs()
	if len(addrs) == 0 {
		t.Fatalf("expected host to expose listen addresses")
	}

	hasTCP := false
	hasQUIC := false
	for _, addr := range addrs {
		addrStr := addr.String()
		if strings.Contains(addrStr, "/tcp/") {
			hasTCP = true
		}
		if strings.Contains(addrStr, "/quic-v1") {
			hasQUIC = true
		}
	}
	if !hasTCP {
		t.Fatalf("expected TCP listen address, got %v", addrs)
	}
	if !hasQUIC {
		t.Fatalf("expected QUIC listen address, got %v", addrs)
	}
}

func newTestNetworkManager(t *testing.T, trackerURL string) (*NetworkManager, func()) {
	t.Helper()
	identity, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatalf("generate test identity: %v", err)
	}

	musicDir := t.TempDir()
	lib, err := library.NewLibrary(musicDir)
	if err != nil {
		t.Fatalf("new library: %v", err)
	}

	port := freeTCPPort(t)
	v := viper.New()
	cfg := &appconfig.Config{
		Host:           "127.0.0.1",
		Port:           port,
		Rendezvous:     constants.DefaultRendezvous,
		DHTEnabled:     false,
		MaxPeers:       constants.DefaultMaxPeers,
		BootstrapPeers: nil,
		MusicDir:       musicDir,
		Volume:         constants.DefaultVolume,
		TUI:            false,
		Network:        true,
		LogLevel:       "error",
	}

	nm, err := newNetworkWithIdentity(cfg, v, lib, musicDir, nil, identity)
	if err != nil {
		t.Fatalf("new network: %v", err)
	}

	cleanup := func() {
		_ = lib.Close()
		_ = nm.Close()
	}
	return nm, cleanup
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for free port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
