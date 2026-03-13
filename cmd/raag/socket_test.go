package main

import (
	"encoding/json"
	"testing"

	"github.com/p-society/raag/internal/socket"
)

func TestTrySocketAndPrintNetworkStatusUnavailable(t *testing.T) {
	client := NewSocketClient()
	if client.IsAvailable() {
		t.Skip("daemon socket is available in environment; skipping unavailable-socket test")
	}
	if trySocketAndPrintNetworkStatus() {
		t.Fatalf("expected network status query to fail without daemon socket")
	}
}

func TestSocketResponseNetworkStatusDecodesShape(t *testing.T) {
	resp := socket.Response{
		Success: true,
		Data: socket.NetworkStatus{
			SelfID:        "peer-a",
			ListenAddr:    "/ip4/127.0.0.1/tcp/45678",
			Mode:          "networked",
			TrackerURL:    "http://localhost:8080",
			AuthPublicKey: "abcd",
		},
	}

	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	data, ok := decoded["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %#v", decoded["data"])
	}
	if got := data["self_id"]; got != "peer-a" {
		t.Fatalf("self_id = %v, want peer-a", got)
	}
	if got := data["tracker_url"]; got != "http://localhost:8080" {
		t.Fatalf("tracker_url = %v, want http://localhost:8080", got)
	}
}
