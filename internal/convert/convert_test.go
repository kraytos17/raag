package convert

import (
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/domain"
)

// TestPeerIDRoundTrip is a regression test for the smoke-test finding where a
// persisted peer's ID was corrupted on every proto round-trip: the proto->domain
// direction used a raw type cast instead of peer.Decode, producing a different
// .String() value (e.g. "CovLVG..." vs "12D3KooW...") so the same peer appeared
// twice in `raag peers list`.
func TestPeerIDRoundTrip(t *testing.T) {
	// Use a real libp2p peer ID, not an ASCII fake.
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, 0)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pid, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		t.Fatalf("peer id from key: %v", err)
	}

	info := &domain.PeerInfo{
		ID:       pid,
		Addrs:    []string{"/ip4/127.0.0.1/tcp/7844"},
		LastSeen: time.Now(),
	}

	// Round-trip through the proto forms used for persistence.
	back := ProtoToPeerInfo(PeerInfoToProto(info))
	if back == nil {
		t.Fatal("ProtoToPeerInfo returned nil")
	}
	if back.ID != info.ID {
		t.Fatalf("peer ID corrupted on round-trip: got %q, want %q (String: %s vs %s)",
			back.ID, info.ID, back.ID.String(), info.ID.String())
	}
}

// TestPeerScoreRoundTrip exercises the same corruption guard for PeerScore.
func TestPeerScoreRoundTrip(t *testing.T) {
	priv, _, err := crypto.GenerateKeyPair(crypto.Ed25519, 0)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pid, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		t.Fatalf("peer id from key: %v", err)
	}

	s := &domain.PeerScore{
		PeerID:       pid,
		AvgLatency:   time.Millisecond * 5,
		AvgBandwidth: 1_000_000,
		SuccessCount: 3,
	}

	back := ProtoToPeerScore(PeerScoreToProto(s))
	if back == nil {
		t.Fatal("ProtoToPeerScore returned nil")
	}
	if back.PeerID != s.PeerID {
		t.Fatalf("peer ID corrupted on score round-trip: got %q, want %q", back.PeerID, s.PeerID)
	}
}

// TestDecodePeerID_Invalid verifies a bad base58 string yields the empty ID
// rather than a corrupted non-empty one.
func TestDecodePeerID_Invalid(t *testing.T) {
	if got := decodePeerID("not-a-real-peer-id"); got != "" {
		t.Fatalf("decodePeerID(invalid) = %q, want empty", got)
	}
}
