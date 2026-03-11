package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/logger"
	"github.com/spf13/cobra"
)

func ensureNetwork(cmd *cobra.Command) error {
	if netMgr == nil {
		if err := initializeApp(cmd); err != nil {
			return fmt.Errorf("initializing: %w", err)
		}
	}
	return nil
}

func ensurePlaylist(cmd *cobra.Command) error {
	if pm == nil || lib == nil {
		if err := initializeApp(cmd); err != nil {
			return fmt.Errorf("initializing: %w", err)
		}
	}
	return nil
}

func playCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "play <song>",
		Short: "Play a song from the library",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			songTitle := args[0]
			song, err := lib.FindSong(songTitle)
			if err != nil {
				logger.Error("song not found", "error", err)
				return
			}

			p.AddToQueue(song)
			if p.GetCurrentSong() == nil {
				if err := p.PlayQueue(); err != nil {
					logger.Error("failed to play queue", "error", err)
				}
			}
		},
	}
}

func pauseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause playback",
		Run: func(cmd *cobra.Command, args []string) {
			p.Pause()
		},
	}
}

func resumeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume playback",
		Run: func(cmd *cobra.Command, args []string) {
			p.Resume()
		},
	}
}

func stopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop playback and clear queue",
		Run: func(cmd *cobra.Command, args []string) {
			p.Stop()
		},
	}
}

func nextCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "next",
		Short: "Skip to next song",
		Run: func(cmd *cobra.Command, args []string) {
			if err := p.Next(); err != nil {
				logger.Error("failed to play next song", "error", err)
			}
		},
	}
}

func previousCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "previous",
		Short: "Go to previous song",
		Run: func(cmd *cobra.Command, args []string) {
			if err := p.Previous(); err != nil {
				logger.Error("failed to play previous song", "error", err)
			}
		},
	}
}

func queueCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "queue",
		Short: "Manage queue",
		Run: func(cmd *cobra.Command, args []string) {
			queue := p.GetQueue()
			if len(queue) == 0 {
				logger.Info("queue is empty")
				return
			}

			current := p.GetCurrentSong()
			logger.Info("current queue")
			for i, song := range queue {
				marker := "  "
				if current != nil && current.Title == song.Title {
					marker = "> "
				}
				logger.Info("song", "index", i+1, "marker", marker, "title", song.Title, "artist", song.Artist)
			}
		},
	}
}

func volumeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "volume [0-100]",
		Short: "Set volume (0-100) or show current volume if no argument",
		Args:  cobra.RangeArgs(0, 1),
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				vol := p.GetVolume()
				logger.Info("current volume", "volume", vol)
				return
			}

			vol, err := strconv.ParseFloat(args[0], 64)
			if err != nil {
				logger.Error("invalid volume level", "error", err)
				return
			}
			if err := p.SetVolume(vol); err != nil {
				logger.Error("failed to set volume", "error", err)
			}
		},
	}
}

func seekCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "seek [seconds]",
		Short: "Seek to position in seconds",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			pos, err := strconv.Atoi(args[0])
			if err != nil {
				logger.Error("invalid position", "error", err)
				return
			}

			if err := p.Seek(pos); err != nil {
				logger.Error("failed to seek", "error", err)
			}
		},
	}
}

func nowplayingCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "nowplaying",
		Short: "Show current playing song",
		Run: func(cmd *cobra.Command, args []string) {
			song := p.GetCurrentSong()
			if song == nil {
				logger.Info("no song playing")
				return
			}

			pos := p.GetPosition()
			dur := p.GetDuration()
			vol := p.GetVolume()
			playing := p.IsPlaying()
			paused := p.IsPaused()

			status := "Playing"
			if paused {
				status = "Paused"
			} else if !playing {
				status = "Stopped"
			}

			logger.Info("now playing", "status", status)
			logger.Info("title", "title", song.Title)
			logger.Info("artist", "artist", song.Artist)
			logger.Info("album", "album", song.Album)
			logger.Info("position", "position", pos, "duration", dur)
			logger.Info("volume", "volume", vol)
		},
	}
}

func peersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "peers",
		Short: "Manage peers and discovery",
	}

	cmd.AddCommand(peersListCommand())
	cmd.AddCommand(peersInfoCommand())
	cmd.AddCommand(peersConnectCommand())
	cmd.AddCommand(peersDisconnectCommand())
	cmd.AddCommand(peersTrackerCommand())
	cmd.AddCommand(peersBootstrapCommand())

	return cmd
}

func peersListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List connected peers",
		Run: func(cmd *cobra.Command, args []string) {
			if trySocketAndPrintPeers() {
				return
			}
			if err := ensureNetwork(cmd); err != nil {
				logger.Error("initializing network", "error", err)
				return
			}

			timeout := 10 * time.Second
			logger.Info("waiting for peer connections", "timeout", timeout)

			waited := false
			for i := 0; i < int(timeout.Seconds()); i++ {
				if netMgr.IsOnline() {
					break
				}
				time.Sleep(1 * time.Second)
				waited = true
			}
			if waited {
				count := netMgr.GetConnectedPeerCount()
				logger.Info("connected peers found", "count", count)
			}

			peers := netMgr.GetPeers()
			if len(peers) == 0 {
				logger.Info("no peers connected")
				knownPeers := netMgr.GetAllKnownPeers()
				if len(knownPeers) > 0 {
					logger.Info("known peers from discovery", "count", len(knownPeers))
				}
				return
			}

			logger.Info("connected peers", "count", len(peers))
			for _, p := range peers {
				logger.Info("peer", "id", p.ID)
			}
		},
	}
}

func trySocketAndPrintPeers() bool {
	client := NewSocketClient()
	if !client.IsAvailable() {
		return false
	}

	resp, err := client.Query("peers list")
	if err != nil {
		return false
	}
	if !resp.Success {
		return false
	}

	peers, ok := resp.Data.([]any)
	if !ok {
		logger.Error("parsing peer data from daemon", "error", "invalid format")
		return false
	}

	logger.Info("connected peers", "count", len(peers))
	for _, p := range peers {
		peerMap := p.(map[string]any)
		logger.Info("peer", "id", peerMap["id"])
	}
	return true
}

func peersInfoCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show all peer info (self + known peers)",
		Run: func(cmd *cobra.Command, args []string) {
			if trySocketAndPrintPeersInfo() {
				return
			}
			if err := ensureNetwork(cmd); err != nil {
				logger.Error("initializing network", "error", err)
				return
			}

			timeout := 10 * time.Second
			logger.Info("waiting for peer connections", "timeout", timeout)

			for i := 0; i < int(timeout.Seconds()); i++ {
				if netMgr.IsOnline() {
					break
				}
				time.Sleep(1 * time.Second)
			}

			peerID := netMgr.GetPeerID()
			multiaddr := netMgr.GetMultiaddr()

			logger.Info("self info")
			logger.Info("peer id", "id", peerID)
			logger.Info("multiaddr", "addr", multiaddr)

			connectedPeers := netMgr.GetPeers()
			connectedPeerIDs := make(map[peer.ID]bool)
			for _, p := range connectedPeers {
				connectedPeerIDs[p.ID] = true
			}

			allKnownPeers := netMgr.GetAllKnownPeers()
			logger.Info("connected peers", "count", len(connectedPeers))
			if len(connectedPeers) == 0 {
				logger.Info("no peers connected")
			}
			for _, p := range connectedPeers {
				logger.Info("peer", "id", p.ID)
			}

			logger.Info("known peers from discovery", "count", len(allKnownPeers))
			if len(allKnownPeers) == 0 {
				logger.Info("no known peers")
			}
			for _, p := range allKnownPeers {
				status := "Connected"
				if !connectedPeerIDs[p.ID] {
					status = "Discovered (not connected)"
				}
				logger.Info("peer", "id", p.ID, "status", status)
			}
		},
	}
}

func trySocketAndPrintPeersInfo() bool {
	client := NewSocketClient()
	if !client.IsAvailable() {
		return false
	}

	resp, err := client.Query("peers info")
	if err != nil {
		return false
	}
	if !resp.Success {
		return false
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		logger.Error("parsing peer info from daemon", "error", "invalid format")
		return false
	}

	self, ok := data["self"].(map[string]any)
	if ok {
		logger.Info("self info")
		logger.Info("peer id", "id", self["peer_id"])
		logger.Info("multiaddr", "addr", self["multiaddr"])
	}

	connectedPeers, ok := data["connected_peers"].([]any)
	if ok {
		logger.Info("connected peers", "count", len(connectedPeers))
		if len(connectedPeers) == 0 {
			logger.Info("no peers connected")
		}
		for _, p := range connectedPeers {
			peerMap := p.(map[string]any)
			logger.Info("peer", "id", peerMap["ID"])
		}
	}

	knownPeers, ok := data["known_peers"].([]any)
	if ok {
		logger.Info("known peers from discovery", "count", len(knownPeers))
		if len(knownPeers) == 0 {
			logger.Info("no known peers")
		}
		for _, p := range knownPeers {
			peerMap := p.(map[string]any)
			logger.Info("peer", "id", peerMap["ID"], "status", "Discovered")
		}
	}
	return true
}

func peersConnectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "connect <multiaddr>",
		Short: "Connect to a peer by multiaddr",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensureNetwork(cmd); err != nil {
				logger.Error("initializing network", "error", err)
				return
			}

			addr := args[0]
			addrInfo, err := peer.AddrInfoFromString(addr)
			if err != nil {
				logger.Error("invalid multiaddr", "error", err)
				return
			}
			if err := netMgr.Connect(cmd.Context(), *addrInfo); err != nil {
				logger.Error("failed to connect to peer", "error", err)
				return
			}
		},
	}
}

func peersDisconnectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect <peerID>",
		Short: "Disconnect from a peer",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensureNetwork(cmd); err != nil {
				logger.Error("initializing network", "error", err)
				return
			}

			peerID, err := peer.Decode(args[0])
			if err != nil {
				logger.Error("invalid peer ID", "error", err)
				return
			}
			if err := netMgr.Disconnect(peerID); err != nil {
				logger.Error("failed to disconnect from peer", "error", err)
				return
			}
			logger.Info("disconnected from peer", "id", peerID)
		},
	}
}

func peersTrackerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "tracker <url>",
		Short: "Set tracker URL",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			trackerURL := args[0]
			logger.Info("setting tracker URL", "url", trackerURL)
			if err := ensureNetwork(cmd); err != nil {
				logger.Error("initializing network", "error", err)
				return
			}

			ctx := context.Background()
			netMgr.UpdateTrackerURL(ctx, trackerURL)
			logger.Info("tracker URL updated successfully")
		},
	}
}

func peersBootstrapCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap <multiaddr>",
		Short: "Add bootstrap peer",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			multiaddrStr := args[0]
			logger.Info("adding bootstrap peer", "multiaddr", multiaddrStr)

			ctx := context.Background()
			if err := netMgr.AddBootstrapPeer(ctx, multiaddrStr); err != nil {
				logger.Error("failed to add bootstrap peer", "error", err)
			} else {
				logger.Info("bootstrap peer added successfully")
			}
		},
	}
}

func libraryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "library",
		Short: "Manage music library",
	}

	cmd.AddCommand(libraryListCommand())
	cmd.AddCommand(librarySearchCommand())
	cmd.AddCommand(libraryRescanCommand())
	cmd.AddCommand(libraryAddCommand())
	cmd.AddCommand(libraryRemoveCommand())

	return cmd
}

func libraryListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all songs in library",
		Run: func(cmd *cobra.Command, args []string) {
			songs := lib.ListSongs()
			if len(songs) == 0 {
				logger.Info("library is empty")
				return
			}

			logger.Info("library contents", "count", len(songs))
			for i, song := range songs {
				logger.Info("song", "index", i+1, "title", song.Title, "artist", song.Artist, "album", song.Album)
			}
		},
	}
}

func librarySearchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "search [query]",
		Short: "Search songs in library",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			query := strings.ToLower(args[0])
			songs := lib.ListSongs()

			var results []string
			for _, song := range songs {
				if strings.Contains(strings.ToLower(song.Title), query) ||
					strings.Contains(strings.ToLower(song.Artist), query) ||
					strings.Contains(strings.ToLower(song.Album), query) {
					results = append(results, fmt.Sprintf("%s - %s (%s)", song.Title, song.Artist, song.Album))
				}
			}
			if len(results) == 0 {
				logger.Info("no songs found matching query", "query", args[0])
				return
			}

			logger.Info("matching songs found", "count", len(results))
			for _, r := range results {
				logger.Info("result", "song", r)
			}
		},
	}
}

func libraryRescanCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rescan",
		Short: "Rescan music directory",
		Run: func(cmd *cobra.Command, args []string) {
			logger.Info("rescanning music library")
			musicDir := lib.GetMusicDir()
			if err := lib.ScanMusicLibrary(musicDir); err != nil {
				logger.Error("failed to rescan library", "error", err)
				return
			}

			songs := lib.ListSongs()
			logger.Info("library rescanned", "song_count", len(songs))
		},
	}
}

func libraryAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add <path>",
		Short: "Add a song to library",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			path := args[0]
			if err := lib.AddSong(path); err != nil {
				logger.Error("failed to add song", "error", err)
				return
			}
			logger.Info("song added", "path", path)
		},
	}
}

func libraryRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <title>",
		Short: "Remove a song from library",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			title := args[0]
			if err := lib.RemoveSong(title); err != nil {
				logger.Error("failed to remove song", "error", err)
				return
			}
			logger.Info("song removed from library", "title", title)
		},
	}
}

func playlistCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlist",
		Short: "Manage playlists",
	}

	cmd.AddCommand(playlistCreateCommand())
	cmd.AddCommand(playlistDeleteCommand())
	cmd.AddCommand(playlistListCommand())
	cmd.AddCommand(playlistAddCommand())
	cmd.AddCommand(playlistRemoveCommand())
	cmd.AddCommand(playlistSongsCommand())
	cmd.AddCommand(playlistPlayCommand())

	return cmd
}

func playlistCreateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new playlist",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensurePlaylist(cmd); err != nil {
				logger.Error("initializing playlist", "error", err)
				return
			}

			name := args[0]
			if err := pm.Create(name); err != nil {
				logger.Error("failed to create playlist", "error", err)
				return
			}
			logger.Info("playlist created", "name", name)
		},
	}
}

func playlistDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a playlist",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensurePlaylist(cmd); err != nil {
				logger.Error("initializing playlist", "error", err)
				return
			}

			name := args[0]
			if err := pm.Delete(name); err != nil {
				logger.Error("failed to delete playlist", "error", err)
				return
			}
			logger.Info("playlist deleted", "name", name)
		},
	}
}

func playlistListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all playlists",
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensurePlaylist(cmd); err != nil {
				logger.Error("initializing playlist", "error", err)
				return
			}

			names := pm.List()
			if len(names) == 0 {
				logger.Info("no playlists found")
				return
			}

			logger.Info("playlists")
			for _, name := range names {
				logger.Info("playlist", "name", name)
			}
		},
	}
}

func playlistAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add <playlist> <song>",
		Short: "Add a song to a playlist",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensurePlaylist(cmd); err != nil {
				logger.Error("initializing playlist", "error", err)
				return
			}

			playlistName := args[0]
			songTitle := args[1]
			song, err := lib.FindSong(songTitle)
			if err != nil {
				logger.Error("song not found", "error", err)
				return
			}
			if err := pm.AddSong(playlistName, song); err != nil {
				logger.Error("failed to add song to playlist", "error", err)
				return
			}
			logger.Info("song added to playlist", "song", songTitle, "playlist", playlistName)
		},
	}
}

func playlistRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <playlist> <index>",
		Short: "Remove a song from playlist by index",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensurePlaylist(cmd); err != nil {
				logger.Error("initializing playlist", "error", err)
				return
			}

			playlistName := args[0]
			index, err := strconv.Atoi(args[1])
			if err != nil {
				logger.Error("invalid index", "error", err)
				return
			}
			if err := pm.RemoveSong(playlistName, index-1); err != nil {
				logger.Error("failed to remove song from playlist", "error", err)
				return
			}
			logger.Info("song removed from playlist", "index", index, "playlist", playlistName)
		},
	}
}

func playlistSongsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "songs <playlist>",
		Short: "List songs in a playlist",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensurePlaylist(cmd); err != nil {
				logger.Error("initializing playlist", "error", err)
				return
			}

			playlistName := args[0]
			songs, err := pm.GetSongs(playlistName)
			if err != nil {
				logger.Error("failed to get playlist songs", "error", err)
				return
			}
			if len(songs) == 0 {
				logger.Info("playlist is empty", "playlist", playlistName)
				return
			}

			logger.Info("songs in playlist", "playlist", playlistName)
			for i, song := range songs {
				logger.Info("song", "index", i+1, "title", song.Title, "artist", song.Artist)
			}
		},
	}
}

func playlistPlayCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "play <playlist>",
		Short: "Play all songs in a playlist",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensurePlaylist(cmd); err != nil {
				logger.Error("initializing playlist", "error", err)
				return
			}

			playlistName := args[0]
			songs, err := pm.GetSongs(playlistName)
			if err != nil {
				logger.Error("failed to get playlist songs", "error", err)
				return
			}
			if len(songs) == 0 {
				logger.Info("playlist is empty", "playlist", playlistName)
				return
			}

			for _, song := range songs {
				p.AddToQueue(song)
			}
			if err := p.PlayQueue(); err != nil {
				logger.Error("failed to play playlist", "error", err)
				return
			}
			logger.Info("playing playlist", "playlist", playlistName, "song_count", len(songs))
		},
	}
}

func shareCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "share [peerID] [song title]",
		Short: "Share a song with a peer",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensureNetwork(cmd); err != nil {
				logger.Error("initializing network", "error", err)
				return
			}

			peerID := args[0]
			songTitle := args[1]

			logger.Info("sharing song with peer", "song", songTitle, "peer", peerID)
			song, err := lib.FindSong(songTitle)
			if err != nil {
				logger.Error("song not found in library", "error", err)
				return
			}

			logger.Info("song found in library", "title", song.Title, "artist", song.Artist)
			peerInfo, err := peer.AddrInfoFromString(peerID)
			if err != nil {
				logger.Error("failed to parse peer ID", "error", err)
				return
			}

			logger.Info("peer info parsed", "id", peerInfo.ID)
			if err := netMgr.ShareSong(peerInfo, song); err != nil {
				logger.Error("failed to share song", "error", err)
				return
			}
			logger.Info("song shared successfully", "song", songTitle, "peer", peerID)
		},
	}
}

func statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		Run: func(cmd *cobra.Command, args []string) {
			if trySocketAndPrintStatus() {
				return
			}

			if err := ensureNetwork(cmd); err != nil {
				logger.Error("initializing network", "error", err)
				return
			}

			logger.Info("daemon status (standalone mode)")
			logger.Info("running", "value", true)
			logger.Info("peer count", "count", netMgr.GetConnectedPeerCount())
			logger.Info("network online", "status", netMgr.IsOnline())
		},
	}
}

func trySocketAndPrintStatus() bool {
	client := NewSocketClient()
	if !client.IsAvailable() {
		return false
	}

	resp, err := client.Query("status")
	if err != nil {
		return false
	}

	if !resp.Success {
		return false
	}

	data, ok := resp.Data.(map[string]any)
	if !ok {
		logger.Error("parsing status from daemon", "error", "invalid format")
		return false
	}

	logger.Info("daemon status")
	logger.Info("running", "value", data["running"])
	logger.Info("peer count", "count", data["peer_count"])
	logger.Info("network online", "status", data["connected"])
	logger.Info("uptime", "value", data["uptime"])
	logger.Info("version", "value", data["version"])
	return true
}

func configCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	cmd.AddCommand(configShowCommand())
	cmd.AddCommand(configSetCommand())
	cmd.AddCommand(configResetCommand())

	return cmd
}

func configShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		Run: func(cmd *cobra.Command, args []string) {
			if cfg == nil {
				logger.Info("current configuration (default)")
				logger.Info("music_dir", "value", "./music")
				logger.Info("volume", "value", 50)
				logger.Info("tui_enabled", "value", false)
				logger.Info("wifi_mode", "value", false)
				logger.Info("offline", "value", true)
				logger.Info("rendezvous", "value", "raag-music-share")
				logger.Info("host", "value", "127.0.0.1")
				logger.Info("port", "value", 0)
				logger.Info("log_level", "value", "info")
				return
			}

			logger.Info("current configuration")
			logger.Info("music_dir", "value", cfg.MusicDir)
			logger.Info("volume", "value", cfg.Volume)
			logger.Info("tui_enabled", "value", cfg.TUI)
			logger.Info("wifi_mode", "value", cfg.Wifi)
			logger.Info("offline", "value", cfg.Offline)
			logger.Info("rendezvous", "value", cfg.Rendezvous)
			logger.Info("host", "value", cfg.Host)
			logger.Info("port", "value", cfg.Port)
			logger.Info("log_level", "value", cfg.LogLevel)
		},
	}
}

func configSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			key := args[0]
			value := args[1]
			switch key {
			case "musicdir":
				cfg.MusicDir = value
			case "volume":
				v, err := strconv.Atoi(value)
				if err != nil || v < 0 || v > 100 {
					logger.Error("volume must be 0-100", "error", "invalid value")
					return
				}
				cfg.Volume = v
			case "tui":
				v, err := strconv.ParseBool(value)
				if err != nil {
					logger.Error("tui must be true or false", "error", "invalid value")
					return
				}
				cfg.TUI = v
			case "wifi":
				v, err := strconv.ParseBool(value)
				if err != nil {
					logger.Error("wifi must be true or false", "error", "invalid value")
					return
				}
				cfg.Wifi = v
			case "offline":
				v, err := strconv.ParseBool(value)
				if err != nil {
					logger.Error("offline must be true or false", "error", "invalid value")
					return
				}
				cfg.Offline = v
			case "rendezvous":
				cfg.Rendezvous = value
			case "host":
				cfg.Host = value
			case "port":
				v, err := strconv.Atoi(value)
				if err != nil || v < 0 || v > 65535 {
					logger.Error("port must be 0-65535", "error", "invalid value")
					return
				}
				cfg.Port = v
			case "loglevel":
				if value != "debug" && value != "info" && value != "warn" && value != "error" {
					logger.Error("log_level must be debug, info, warn, or error", "error", "invalid value")
					return
				}
				cfg.LogLevel = value
			default:
				logger.Error("unknown config key", "key", key)
				logger.Info("available keys: musicdir, volume, tui, wifi, offline, rendezvous, host, port, loglevel")
				return
			}

			if err := config.SaveConfig(v, cfg); err != nil {
				logger.Error("failed to save config", "error", err)
				return
			}
			logger.Info("config updated", "key", key, "value", value)
		},
	}
}

func configResetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset configuration to defaults",
		Run: func(cmd *cobra.Command, args []string) {
			cfg.MusicDir = "./music"
			cfg.Volume = 50
			cfg.TUI = true
			cfg.Wifi = false
			cfg.Offline = true
			cfg.Rendezvous = "raag-music-share"
			cfg.Host = "127.0.0.1"
			cfg.Port = 0
			cfg.LogLevel = "info"

			if err := config.SaveConfig(v, cfg); err != nil {
				logger.Error("failed to save config", "error", err)
				return
			}
			logger.Info("configuration reset to defaults")
		},
	}
}
