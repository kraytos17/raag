package tui

import (
	"strings"
	"testing"

	pb "github.com/p-society/raag/proto/gen"
)

func TestPeerItem_Title_ConnectedDot(t *testing.T) {
	p := PeerItem{Peer: &pb.Peer{Id: "1234567890", Connected: true}}
	if !strings.HasPrefix(p.Title(), "●") {
		t.Errorf("connected title = %q, want ● prefix", p.Title())
	}
	if !strings.Contains(p.Title(), "Peer 12345678") {
		t.Errorf("connected title = %q, want short id", p.Title())
	}
}

func TestPeerItem_Title_DisconnectedDot(t *testing.T) {
	p := PeerItem{Peer: &pb.Peer{Id: "1234567890", Connected: false}}
	if !strings.HasPrefix(p.Title(), "○") {
		t.Errorf("disconnected title = %q, want ○ prefix", p.Title())
	}
}

func TestPeerItem_Description_ShowsLatencyBandwidthScore(t *testing.T) {
	p := PeerItem{Peer: &pb.Peer{
		Id:        "peer1",
		Connected: true,
		Score: &pb.PeerScore{
			AvgLatencyMs: 12,
			AvgBandwidth: 1_200_000,
			Score:        87.0,
		},
	}}
	desc := p.Description()
	for _, want := range []string{"connected", "12ms", "1.2MB/s", "Score: 87.0"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description = %q, want to contain %q", desc, want)
		}
	}
}

func TestPeerItem_Description_NoScore(t *testing.T) {
	p := PeerItem{Peer: &pb.Peer{Id: "peer1", Connected: false}}
	if got := p.Description(); got != "disconnected" {
		t.Errorf("description = %q, want %q", got, "disconnected")
	}
}

func TestPeerItem_Description_NilPeer(t *testing.T) {
	var p PeerItem
	if got := p.Description(); got != "" {
		t.Errorf("description = %q, want empty", got)
	}
}
