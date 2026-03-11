package socket

import (
	"github.com/libp2p/go-libp2p/core/peer"
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
