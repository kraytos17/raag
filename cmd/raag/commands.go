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
				logger.Errorf("song not found error=%v", err)
				return
			}

			p.AddToQueue(song)
			if p.GetCurrentSong() == nil {
				if err := p.PlayQueue(); err != nil {
					logger.Errorf("failed to play queue error=%v", err)
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
				logger.Errorf("failed to play next song error=%v", err)
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
				logger.Errorf("failed to play previous song error=%v", err)
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
				logger.Infof("song index=%d marker=%s title=%s artist=%s", i+1, marker, song.Title, song.Artist)
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
				logger.Infof("current volume volume=%v", vol)
				return
			}

			vol, err := strconv.ParseFloat(args[0], 64)
			if err != nil {
				logger.Errorf("invalid volume level error=%v", err)
				return
			}
			if err := p.SetVolume(vol); err != nil {
				logger.Errorf("failed to set volume error=%v", err)
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
				logger.Errorf("invalid position error=%v", err)
				return
			}

			if err := p.Seek(pos); err != nil {
				logger.Errorf("failed to seek error=%v", err)
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

			logger.Infof("now playing status=%s", status)
			logger.Infof("title title=%s", song.Title)
			logger.Infof("artist artist=%s", song.Artist)
			logger.Infof("album album=%s", song.Album)
			logger.Infof("position position=%v duration=%v", pos, dur)
			logger.Infof("volume volume=%v", vol)
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
				logger.Errorf("initializing network error=%v", err)
				return
			}

			timeout := 10 * time.Second
			logger.Infof("waiting for peer connections timeout=%v", timeout)

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
				logger.Infof("connected peers found count=%d", count)
			}

			peers := netMgr.GetPeers()
			if len(peers) == 0 {
				logger.Info("no peers connected")
				knownPeers := netMgr.GetAllKnownPeers()
				if len(knownPeers) > 0 {
					logger.Infof("known peers from discovery count=%d", len(knownPeers))
				}
				return
			}

			logger.Infof("connected peers count=%d", len(peers))
			for _, p := range peers {
				logger.Infof("peer id=%s", p.ID)
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
		logger.Errorf("parsing peer data from daemon error=invalid format")
		return false
	}

	logger.Infof("connected peers count=%d", len(peers))
	for _, p := range peers {
		peerMap := p.(map[string]any)
		logger.Infof("peer id=%v", peerMap["id"])
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
				logger.Errorf("initializing network error=%v", err)
				return
			}

			timeout := 10 * time.Second
			logger.Infof("waiting for peer connections timeout=%v", timeout)

			for i := 0; i < int(timeout.Seconds()); i++ {
				if netMgr.IsOnline() {
					break
				}
				time.Sleep(1 * time.Second)
			}

			peerID := netMgr.GetPeerID()
			multiaddr := netMgr.GetMultiaddr()

			logger.Info("self info")
			logger.Infof("peer id id=%s", peerID)
			logger.Infof("multiaddr addr=%s", multiaddr)

			connectedPeers := netMgr.GetPeers()
			connectedPeerIDs := make(map[peer.ID]bool)
			for _, p := range connectedPeers {
				connectedPeerIDs[p.ID] = true
			}

			allKnownPeers := netMgr.GetAllKnownPeers()
			logger.Infof("connected peers count=%d", len(connectedPeers))
			if len(connectedPeers) == 0 {
				logger.Info("no peers connected")
			}
			for _, p := range connectedPeers {
				logger.Infof("peer id=%s", p.ID)
			}

			logger.Infof("known peers from discovery count=%d", len(allKnownPeers))
			if len(allKnownPeers) == 0 {
				logger.Info("no known peers")
			}
			for _, p := range allKnownPeers {
				status := "Connected"
				if !connectedPeerIDs[p.ID] {
					status = "Discovered (not connected)"
				}
				logger.Infof("peer id=%s status=%s", p.ID, status)
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
		logger.Errorf("parsing peer info from daemon error=invalid format")
		return false
	}

	self, ok := data["self"].(map[string]any)
	if ok {
		logger.Info("self info")
		logger.Infof("peer id id=%v", self["peer_id"])
		logger.Infof("multiaddr addr=%v", self["multiaddr"])
	}

	connectedPeers, ok := data["connected_peers"].([]any)
	if ok {
		logger.Infof("connected peers count=%d", len(connectedPeers))
		if len(connectedPeers) == 0 {
			logger.Info("no peers connected")
		}
		for _, p := range connectedPeers {
			peerMap := p.(map[string]any)
			logger.Infof("peer id=%v", peerMap["ID"])
		}
	}

	knownPeers, ok := data["known_peers"].([]any)
	if ok {
		logger.Infof("known peers from discovery count=%d", len(knownPeers))
		if len(knownPeers) == 0 {
			logger.Info("no known peers")
		}
		for _, p := range knownPeers {
			peerMap := p.(map[string]any)
			logger.Infof("peer id=%v status=%s", peerMap["ID"], "Discovered")
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
				logger.Errorf("initializing network error=%v", err)
				return
			}

			addr := args[0]
			addrInfo, err := peer.AddrInfoFromString(addr)
			if err != nil {
				logger.Errorf("invalid multiaddr error=%v", err)
				return
			}
			if err := netMgr.Connect(cmd.Context(), *addrInfo); err != nil {
				logger.Errorf("failed to connect to peer error=%v", err)
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
				logger.Errorf("initializing network error=%v", err)
				return
			}

			peerID, err := peer.Decode(args[0])
			if err != nil {
				logger.Errorf("invalid peer ID error=%v", err)
				return
			}
			if err := netMgr.Disconnect(peerID); err != nil {
				logger.Errorf("failed to disconnect from peer error=%v", err)
				return
			}
			logger.Infof("disconnected from peer id=%s", peerID)
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
			logger.Infof("setting tracker URL url=%s", trackerURL)
			if err := ensureNetwork(cmd); err != nil {
				logger.Errorf("initializing network error=%v", err)
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
			logger.Infof("adding bootstrap peer multiaddr=%s", multiaddrStr)

			ctx := context.Background()
			if err := netMgr.AddBootstrapPeer(ctx, multiaddrStr); err != nil {
				logger.Errorf("failed to add bootstrap peer error=%v", err)
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

			logger.Infof("library contents count=%d", len(songs))
			for i, song := range songs {
				logger.Infof("song index=%d title=%s artist=%s album=%s", i+1, song.Title, song.Artist, song.Album)
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
				logger.Infof("no songs found matching query query=%s", args[0])
				return
			}

			logger.Infof("matching songs found count=%d", len(results))
			for _, r := range results {
				logger.Infof("result song=%s", r)
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
				logger.Errorf("failed to rescan library error=%v", err)
				return
			}

			songs := lib.ListSongs()
			logger.Infof("library rescanned song_count=%d", len(songs))
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
				logger.Errorf("failed to add song error=%v", err)
				return
			}
			logger.Infof("song added path=%s", path)
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
				logger.Errorf("failed to remove song error=%v", err)
				return
			}
			logger.Infof("song removed from library title=%s", title)
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
				logger.Errorf("initializing playlist error=%v", err)
				return
			}

			name := args[0]
			if err := pm.Create(name); err != nil {
				logger.Errorf("failed to create playlist error=%v", err)
				return
			}
			logger.Infof("playlist created name=%s", name)
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
				logger.Errorf("initializing playlist error=%v", err)
				return
			}

			name := args[0]
			if err := pm.Delete(name); err != nil {
				logger.Errorf("failed to delete playlist error=%v", err)
				return
			}
			logger.Infof("playlist deleted name=%s", name)
		},
	}
}

func playlistListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all playlists",
		Run: func(cmd *cobra.Command, args []string) {
			if err := ensurePlaylist(cmd); err != nil {
				logger.Errorf("initializing playlist error=%v", err)
				return
			}

			names := pm.List()
			if len(names) == 0 {
				logger.Info("no playlists found")
				return
			}

			logger.Info("playlists")
			for _, name := range names {
				logger.Infof("playlist name=%s", name)
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
				logger.Errorf("initializing playlist error=%v", err)
				return
			}

			playlistName := args[0]
			songTitle := args[1]
			song, err := lib.FindSong(songTitle)
			if err != nil {
				logger.Errorf("song not found error=%v", err)
				return
			}
			if err := pm.AddSong(playlistName, song); err != nil {
				logger.Errorf("failed to add song to playlist error=%v", err)
				return
			}
			logger.Infof("song added to playlist song=%s playlist=%s", songTitle, playlistName)
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
				logger.Errorf("initializing playlist error=%v", err)
				return
			}

			playlistName := args[0]
			index, err := strconv.Atoi(args[1])
			if err != nil {
				logger.Errorf("invalid index error=%v", err)
				return
			}
			if err := pm.RemoveSong(playlistName, index-1); err != nil {
				logger.Errorf("failed to remove song from playlist error=%v", err)
				return
			}
			logger.Infof("song removed from playlist index=%d playlist=%s", index, playlistName)
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
				logger.Errorf("initializing playlist error=%v", err)
				return
			}

			playlistName := args[0]
			songs, err := pm.GetSongs(playlistName)
			if err != nil {
				logger.Errorf("failed to get playlist songs error=%v", err)
				return
			}
			if len(songs) == 0 {
				logger.Infof("playlist is empty playlist=%s", playlistName)
				return
			}

			logger.Infof("songs in playlist playlist=%s", playlistName)
			for i, song := range songs {
				logger.Infof("song index=%d title=%s artist=%s", i+1, song.Title, song.Artist)
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
				logger.Errorf("initializing playlist error=%v", err)
				return
			}

			playlistName := args[0]
			songs, err := pm.GetSongs(playlistName)
			if err != nil {
				logger.Errorf("failed to get playlist songs error=%v", err)
				return
			}
			if len(songs) == 0 {
				logger.Infof("playlist is empty playlist=%s", playlistName)
				return
			}

			for _, song := range songs {
				p.AddToQueue(song)
			}
			if err := p.PlayQueue(); err != nil {
				logger.Errorf("failed to play playlist error=%v", err)
				return
			}
			logger.Infof("playing playlist playlist=%s song_count=%d", playlistName, len(songs))
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
				logger.Errorf("initializing network error=%v", err)
				return
			}

			peerID := args[0]
			songTitle := args[1]

			logger.Infof("sharing song with peer song=%s peer=%s", songTitle, peerID)
			song, err := lib.FindSong(songTitle)
			if err != nil {
				logger.Errorf("song not found in library error=%v", err)
				return
			}

			logger.Infof("song found in library title=%s artist=%s", song.Title, song.Artist)
			peerInfo, err := peer.AddrInfoFromString(peerID)
			if err != nil {
				logger.Errorf("failed to parse peer ID error=%v", err)
				return
			}

			logger.Infof("peer info parsed id=%s", peerInfo.ID)
			if err := netMgr.ShareSong(peerInfo, song); err != nil {
				logger.Errorf("failed to share song error=%v", err)
				return
			}
			logger.Infof("song shared successfully song=%s peer=%s", songTitle, peerID)
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
				logger.Errorf("initializing network error=%v", err)
				return
			}

			logger.Info("daemon status (standalone mode)")
			logger.Infof("running value=%v", true)
			logger.Infof("peer count count=%d", netMgr.GetConnectedPeerCount())
			logger.Infof("network online status=%v", netMgr.IsOnline())
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
		logger.Errorf("parsing status from daemon error=invalid format")
		return false
	}

	logger.Info("daemon status")
	logger.Infof("running value=%v", data["running"])
	logger.Infof("peer count count=%v", data["peer_count"])
	logger.Infof("network online status=%v", data["network_online"])
	logger.Infof("uptime value=%v", data["uptime"])
	logger.Infof("version value=%v", data["version"])
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
				defaults := config.DefaultConfig()
				logger.Info("current configuration (default)")
				logger.Infof("music_dir value=%s", defaults.MusicDir)
				logger.Infof("volume value=%d", defaults.Volume)
				logger.Infof("tui_enabled value=%v", defaults.TUI)
				logger.Infof("wifi_mode value=%v", defaults.Wifi)
				logger.Infof("offline value=%v", defaults.Offline)
				logger.Infof("rendezvous value=%s", defaults.Rendezvous)
				logger.Infof("host value=%s", defaults.Host)
				logger.Infof("port value=%d", defaults.Port)
				logger.Infof("log_level value=%s", defaults.LogLevel)
				return
			}

			logger.Info("current configuration")
			logger.Infof("music_dir value=%s", cfg.MusicDir)
			logger.Infof("volume value=%d", cfg.Volume)
			logger.Infof("tui_enabled value=%v", cfg.TUI)
			logger.Infof("wifi_mode value=%v", cfg.Wifi)
			logger.Infof("offline value=%v", cfg.Offline)
			logger.Infof("rendezvous value=%s", cfg.Rendezvous)
			logger.Infof("host value=%s", cfg.Host)
			logger.Infof("port value=%d", cfg.Port)
			logger.Infof("log_level value=%s", cfg.LogLevel)
		},
	}
}

func configSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if cfg == nil || v == nil {
				loadedV, loadedCfg, err := loadConfigOnly(cmd)
				if err != nil {
					logger.Errorf("failed to initialize config error=%v", err)
					return
				}
				v = loadedV
				cfg = loadedCfg
			}

			key := args[0]
			value := args[1]
			switch key {
			case "musicdir":
				cfg.MusicDir = value
			case "volume":
				v, err := strconv.Atoi(value)
				if err != nil || v < 0 || v > 100 {
					logger.Errorf("volume must be 0-100 error=invalid value")
					return
				}
				cfg.Volume = v
			case "tui":
				v, err := strconv.ParseBool(value)
				if err != nil {
					logger.Errorf("tui must be true or false error=invalid value")
					return
				}
				cfg.TUI = v
			case "wifi":
				v, err := strconv.ParseBool(value)
				if err != nil {
					logger.Errorf("wifi must be true or false error=invalid value")
					return
				}
				cfg.Wifi = v
			case "offline":
				v, err := strconv.ParseBool(value)
				if err != nil {
					logger.Errorf("offline must be true or false error=invalid value")
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
					logger.Errorf("port must be 0-65535 error=invalid value")
					return
				}
				cfg.Port = v
			case "loglevel":
				if value != "debug" && value != "info" && value != "warn" && value != "error" {
					logger.Errorf("log_level must be debug, info, warn, or error error=invalid value")
					return
				}
				cfg.LogLevel = value
			default:
				logger.Errorf("unknown config key key=%s", key)
				logger.Info("available keys: musicdir, volume, tui, wifi, offline, rendezvous, host, port, loglevel")
				return
			}

			if err := config.SaveConfig(v, cfg); err != nil {
				logger.Errorf("failed to save config error=%v", err)
				return
			}
			if key == "loglevel" {
				logger.SetLevel(cfg.LogLevel)
			}
			logger.Infof("config updated key=%s value=%s", key, value)
		},
	}
}

func configResetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset configuration to defaults",
		Run: func(cmd *cobra.Command, args []string) {
			if cfg == nil || v == nil {
				loadedV, loadedCfg, err := loadConfigOnly(cmd)
				if err != nil {
					logger.Errorf("failed to initialize config error=%v", err)
					return
				}
				v = loadedV
				cfg = loadedCfg
			}

			defaults := config.DefaultConfig()
			cfg.MusicDir = defaults.MusicDir
			cfg.Volume = defaults.Volume
			cfg.TUI = defaults.TUI
			cfg.Wifi = defaults.Wifi
			cfg.Offline = defaults.Offline
			cfg.Rendezvous = defaults.Rendezvous
			cfg.Host = defaults.Host
			cfg.Port = defaults.Port
			cfg.FixedPort = defaults.FixedPort
			cfg.ProtocolID = defaults.ProtocolID
			cfg.TrackerURL = defaults.TrackerURL
			cfg.DHTEnabled = defaults.DHTEnabled
			cfg.MaxPeers = defaults.MaxPeers
			cfg.BootstrapPeers = defaults.BootstrapPeers
			cfg.LogLevel = defaults.LogLevel

			if err := config.SaveConfig(v, cfg); err != nil {
				logger.Errorf("failed to save config error=%v", err)
				return
			}

			logger.SetLevel(cfg.LogLevel)
			logger.Info("configuration reset to defaults")
		},
	}
}
