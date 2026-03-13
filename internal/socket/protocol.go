package socket

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/discovery"
	"github.com/p-society/raag/internal/metadata"
)

type Request struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type Response struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

type PeerInfo struct {
	ID        string `json:"id"`
	Multiaddr string `json:"multiaddr"`
	Connected bool   `json:"connected"`
}

type DaemonStatus struct {
	Running   bool   `json:"running"`
	PeerCount int    `json:"peer_count"`
	Connected bool   `json:"network_online"`
	Uptime    string `json:"uptime"`
	Version   string `json:"version"`
}

type LibrarySong struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Path   string `json:"path"`
}

type NetworkStatus struct {
	SelfID         string     `json:"self_id"`
	ListenAddr     string     `json:"listen_addr"`
	Mode           string     `json:"mode"`
	TrackerURL     string     `json:"tracker_url"`
	TrackerStatus  string     `json:"tracker_status"`
	DHTEnabled     bool       `json:"dht_enabled"`
	DHTPeers       int        `json:"dht_peers"`
	MDNSEnabled    bool       `json:"mdns_enabled"`
	MDNSDiscovered int        `json:"mdns_discovered"`
	ConnectedPeers []PeerInfo `json:"connected_peers"`
	KnownPeers     []PeerInfo `json:"known_peers"`
	AuthPublicKey  string     `json:"auth_public_key,omitempty"`
}

func PeerToPeerInfo(p peer.AddrInfo) PeerInfo {
	addr := ""
	if len(p.Addrs) > 0 {
		addr = p.Addrs[0].String()
	}
	return PeerInfo{
		ID:        p.ID.String(),
		Multiaddr: addr,
		Connected: true,
	}
}

func SongToLibrarySong(s metadata.Song) LibrarySong {
	return LibrarySong{
		Title:  s.Title,
		Artist: s.Artist,
		Album:  s.Album,
		Path:   s.Path,
	}
}

func NetworkStatusFromDiscovery(state discovery.NetworkState) NetworkStatus {
	connected := make([]PeerInfo, 0, len(state.ConnectedPeers))
	for _, p := range state.ConnectedPeers {
		connected = append(connected, PeerInfo{
			ID:        p.ID,
			Multiaddr: p.Addr,
			Connected: p.Connected,
		})
	}

	known := make([]PeerInfo, 0, len(state.KnownPeers))
	for _, p := range state.KnownPeers {
		known = append(known, PeerInfo{
			ID:        p.ID,
			Multiaddr: p.Addr,
			Connected: p.Connected,
		})
	}

	return NetworkStatus{
		SelfID:         state.SelfID,
		ListenAddr:     state.ListenAddr,
		Mode:           state.Mode,
		TrackerURL:     state.TrackerURL,
		TrackerStatus:  state.TrackerStatus,
		DHTEnabled:     state.DHTEnabled,
		DHTPeers:       state.DHTPeers,
		MDNSEnabled:    state.MDNSEnabled,
		MDNSDiscovered: state.MDNSDiscovered,
		ConnectedPeers: connected,
		KnownPeers:     known,
		AuthPublicKey:  state.AuthPublicKey,
	}
}
