package cli

import (
	"fmt"
	log "log"
	"strconv"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/p-society/raag/internal/library"
	"github.com/p-society/raag/internal/network"
	"github.com/p-society/raag/internal/player"
	"github.com/p-society/raag/internal/playlist"
	"github.com/p-society/raag/internal/storage"
	"github.com/spf13/cobra"
)

type CLI struct {
	library   *library.Library
	player    *player.Player
	network   *network.NetworkManager
	playlists *playlist.Manager
	storage   *storage.Storage
	rootCmd   *cobra.Command
}

func NewCLI(lib *library.Library, p *player.Player, net *network.NetworkManager, pm *playlist.Manager, store *storage.Storage) *CLI {
	cli := &CLI{
		library:   lib,
		player:    p,
		network:   net,
		playlists: pm,
		storage:   store,
	}
	cli.rootCmd = &cobra.Command{
		Use:   "raag",
		Short: "Raag CLI for decentralized music streaming",
		Run: func(cmd *cobra.Command, args []string) {
			// No default action - just return
		},
	}

	cli.rootCmd.CompletionOptions.DisableDefaultCmd = true
	cli.rootCmd.SetHelpCommand(&cobra.Command{Hidden: true})
	cli.rootCmd.AddCommand(cli.shareCommand())
	cli.rootCmd.AddCommand(cli.playCommand())
	cli.rootCmd.AddCommand(cli.pauseCommand())
	cli.rootCmd.AddCommand(cli.resumeCommand())
	cli.rootCmd.AddCommand(cli.stopCommand())
	cli.rootCmd.AddCommand(cli.nextCommand())
	cli.rootCmd.AddCommand(cli.previousCommand())
	cli.rootCmd.AddCommand(cli.queueCommand())
	cli.rootCmd.AddCommand(cli.volumeCommand())
	cli.rootCmd.AddCommand(cli.seekCommand())
	cli.rootCmd.AddCommand(cli.libraryCommand())
	cli.rootCmd.AddCommand(cli.nowplayingCommand())
	cli.rootCmd.AddCommand(cli.playlistCommand())
	cli.rootCmd.AddCommand(cli.peersCommand())
	cli.rootCmd.AddCommand(cli.configCommand())

	return cli
}

func (c *CLI) shareCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "share [peer ID] [song title]",
		Short: "Share a song with a peer",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			peerID := args[0]
			songTitle := args[1]

			log.Printf("Attempting to share song '%s' with peer %s\n", songTitle, peerID)
			song, err := c.library.FindSong(songTitle)
			if err != nil {
				log.Printf("Error: Song not found in library: %s\n", err)
				return
			}
			log.Printf("Song found in library: %+v\n", song)

			peerInfo, err := peer.AddrInfoFromString(peerID)
			if err != nil {
				log.Printf("Error parsing peer ID: %v\n", err)
				return
			}
			log.Printf("Peer info parsed: %+v\n", peerInfo)

			if err := c.network.ShareSong(peerInfo, song); err != nil {
				log.Printf("Error sharing song: %v\n", err)
				return
			}

			log.Printf("Song '%s' shared successfully with peer %s\n", songTitle, peerID)
		},
	}
}

func (c *CLI) playCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "play [song title]",
		Short: "Play a song from the library",
		Args:  cobra.RangeArgs(0, 1),
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				if err := c.player.PlayQueue(); err != nil {
					log.Printf("Error: %v\n", err)
				}
				return
			}

			songTitle := args[0]
			song, err := c.library.FindSong(songTitle)
			if err != nil {
				log.Printf("Error: Song not found: %v\n", err)
				return
			}

			c.player.AddToQueue(song)
			if c.player.GetCurrentSong() == nil {
				if err := c.player.PlayQueue(); err != nil {
					log.Printf("Error: %v\n", err)
				}
			}
		},
	}
}

func (c *CLI) pauseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause playback",
		Run: func(cmd *cobra.Command, args []string) {
			c.player.Pause()
		},
	}
}

func (c *CLI) resumeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume playback",
		Run: func(cmd *cobra.Command, args []string) {
			c.player.Resume()
		},
	}
}

func (c *CLI) stopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop playback and clear queue",
		Run: func(cmd *cobra.Command, args []string) {
			c.player.Stop()
		},
	}
}

func (c *CLI) nextCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "next",
		Short: "Skip to next song",
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.player.Next(); err != nil {
				log.Printf("Error: %v\n", err)
			}
		},
	}
}

func (c *CLI) previousCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "previous",
		Short: "Go to previous song",
		Run: func(cmd *cobra.Command, args []string) {
			if err := c.player.Previous(); err != nil {
				log.Printf("Error: %v\n", err)
			}
		},
	}
}

func (c *CLI) queueCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "queue",
		Short: "Manage queue",
		Run: func(cmd *cobra.Command, args []string) {
			queue := c.player.GetQueue()
			if len(queue) == 0 {
				log.Println("Queue is empty")
				return
			}

			current := c.player.GetCurrentSong()
			log.Println("Current queue:")
			for i, song := range queue {
				marker := "  "
				if current != nil && current.Title == song.Title {
					marker = "> "
				}
				log.Printf("%s%d. %s - %s\n", marker, i+1, song.Title, song.Artist)
			}
		},
	}
}

func (c *CLI) volumeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "volume [0-100]",
		Short: "Set volume (0-100) or show current volume if no argument",
		Args:  cobra.RangeArgs(0, 1),
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				vol := c.player.GetVolume()
				log.Printf("Current volume: %.0f%%\n", vol)
				return
			}

			vol, err := strconv.ParseFloat(args[0], 64)
			if err != nil {
				log.Printf("Error: Invalid volume level: %v\n", err)
				return
			}
			if err := c.player.SetVolume(vol); err != nil {
				log.Printf("Error: %v\n", err)
			}
		},
	}
}

func (c *CLI) seekCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "seek [seconds]",
		Short: "Seek to position in seconds",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			pos, err := strconv.Atoi(args[0])
			if err != nil {
				log.Printf("Error: Invalid position: %v\n", err)
				return
			}

			if err := c.player.Seek(pos); err != nil {
				log.Printf("Error: %v\n", err)
			}
		},
	}
}

func (c *CLI) libraryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "library",
		Short: "Manage music library",
	}

	cmd.AddCommand(c.libraryListCommand())
	cmd.AddCommand(c.librarySearchCommand())
	cmd.AddCommand(c.libraryRescanCommand())
	cmd.AddCommand(c.libraryAddCommand())
	cmd.AddCommand(c.libraryRemoveCommand())

	return cmd
}

func (c *CLI) libraryListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all songs in library",
		Run: func(cmd *cobra.Command, args []string) {
			songs := c.library.ListSongs()
			if len(songs) == 0 {
				log.Println("Library is empty")
				return
			}

			log.Printf("Library contains %d songs:\n", len(songs))
			for i, song := range songs {
				log.Printf("%d. %s - %s (%s)\n", i+1, song.Title, song.Artist, song.Album)
			}
		},
	}
}

func (c *CLI) librarySearchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "search [query]",
		Short: "Search songs in library",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			query := strings.ToLower(args[0])
			songs := c.library.ListSongs()

			var results []string
			for _, song := range songs {
				if strings.Contains(strings.ToLower(song.Title), query) ||
					strings.Contains(strings.ToLower(song.Artist), query) ||
					strings.Contains(strings.ToLower(song.Album), query) {
					results = append(results, fmt.Sprintf("%s - %s (%s)", song.Title, song.Artist, song.Album))
				}
			}

			if len(results) == 0 {
				log.Printf("No songs found matching '%s'\n", args[0])
				return
			}

			log.Printf("Found %d matching songs:\n", len(results))
			for _, r := range results {
				log.Println(r)
			}
		},
	}
}

func (c *CLI) libraryRescanCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rescan",
		Short: "Rescan music directory",
		Run: func(cmd *cobra.Command, args []string) {
			log.Println("Rescanning music library...")
			musicDir := c.library.GetMusicDir()
			if err := c.library.ScanMusicLibrary(musicDir); err != nil {
				log.Printf("Error rescanning library: %v\n", err)
				return
			}
			songs := c.library.ListSongs()
			log.Printf("Library rescanned. Found %d songs.\n", len(songs))
		},
	}
}

func (c *CLI) libraryAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add <path>",
		Short: "Add a song to library",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			path := args[0]
			if err := c.library.AddSong(path); err != nil {
				log.Printf("Error adding song: %v\n", err)
				return
			}
			log.Printf("Added song from: %s\n", path)
		},
	}
}

func (c *CLI) libraryRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <title>",
		Short: "Remove a song from library",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			title := args[0]
			if err := c.library.RemoveSong(title); err != nil {
				log.Printf("Error removing song: %v\n", err)
				return
			}
			log.Printf("Removed '%s' from library\n", title)
		},
	}
}

func (c *CLI) nowplayingCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "nowplaying",
		Short: "Show current playing song",
		Run: func(cmd *cobra.Command, args []string) {
			song := c.player.GetCurrentSong()
			if song == nil {
				log.Println("No song playing")
				return
			}

			pos := c.player.GetPosition()
			dur := c.player.GetDuration()
			vol := c.player.GetVolume()
			playing := c.player.IsPlaying()
			paused := c.player.IsPaused()

			status := "Playing"
			if paused {
				status = "Paused"
			} else if !playing {
				status = "Stopped"
			}

			log.Printf("Now %s:\n", status)
			log.Printf("  Title:  %s\n", song.Title)
			log.Printf("  Artist: %s\n", song.Artist)
			log.Printf("  Album:  %s\n", song.Album)
			log.Printf("  Position: %d / %d seconds\n", pos, dur)
			log.Printf("  Volume: %.0f%%\n", vol)
		},
	}
}

func (c *CLI) playlistCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlist",
		Short: "Manage playlists",
	}

	cmd.AddCommand(c.playlistCreateCommand())
	cmd.AddCommand(c.playlistDeleteCommand())
	cmd.AddCommand(c.playlistListCommand())
	cmd.AddCommand(c.playlistAddCommand())
	cmd.AddCommand(c.playlistRemoveCommand())
	cmd.AddCommand(c.playlistSongsCommand())
	cmd.AddCommand(c.playlistPlayCommand())

	return cmd
}

func (c *CLI) playlistCreateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new playlist",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if err := c.playlists.Create(name); err != nil {
				log.Printf("Error: %v\n", err)
				return
			}
			log.Printf("Playlist '%s' created\n", name)
		},
	}
}

func (c *CLI) playlistDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a playlist",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			if err := c.playlists.Delete(name); err != nil {
				log.Printf("Error: %v\n", err)
				return
			}
			log.Printf("Playlist '%s' deleted\n", name)
		},
	}
}

func (c *CLI) playlistListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all playlists",
		Run: func(cmd *cobra.Command, args []string) {
			names := c.playlists.List()
			if len(names) == 0 {
				log.Println("No playlists found")
				return
			}
			log.Println("Playlists:")
			for _, name := range names {
				log.Printf("  - %s\n", name)
			}
		},
	}
}

func (c *CLI) playlistAddCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "add <playlist> <song>",
		Short: "Add a song to a playlist",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			playlistName := args[0]
			songTitle := args[1]

			song, err := c.library.FindSong(songTitle)
			if err != nil {
				log.Printf("Error: Song not found: %v\n", err)
				return
			}
			if err := c.playlists.AddSong(playlistName, song); err != nil {
				log.Printf("Error: %v\n", err)
				return
			}
			log.Printf("Added '%s' to playlist '%s'\n", songTitle, playlistName)
		},
	}
}

func (c *CLI) playlistRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <playlist> <index>",
		Short: "Remove a song from playlist by index",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			playlistName := args[0]
			index, err := strconv.Atoi(args[1])
			if err != nil {
				log.Printf("Error: Invalid index: %v\n", err)
				return
			}
			if err := c.playlists.RemoveSong(playlistName, index-1); err != nil {
				log.Printf("Error: %v\n", err)
				return
			}
			log.Printf("Removed song at index %d from playlist '%s'\n", index, playlistName)
		},
	}
}

func (c *CLI) playlistSongsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "songs <playlist>",
		Short: "List songs in a playlist",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			playlistName := args[0]
			songs, err := c.playlists.GetSongs(playlistName)
			if err != nil {
				log.Printf("Error: %v\n", err)
				return
			}
			if len(songs) == 0 {
				log.Printf("Playlist '%s' is empty\n", playlistName)
				return
			}

			log.Printf("Songs in '%s':\n", playlistName)
			for i, song := range songs {
				log.Printf("  %d. %s - %s\n", i+1, song.Title, song.Artist)
			}
		},
	}
}

func (c *CLI) playlistPlayCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "play <playlist>",
		Short: "Play all songs in a playlist",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			playlistName := args[0]
			songs, err := c.playlists.GetSongs(playlistName)
			if err != nil {
				log.Printf("Error: %v\n", err)
				return
			}
			if len(songs) == 0 {
				log.Printf("Playlist '%s' is empty\n", playlistName)
				return
			}

			for _, song := range songs {
				c.player.AddToQueue(song)
			}
			if err := c.player.PlayQueue(); err != nil {
				log.Printf("Error: %v\n", err)
				return
			}
			log.Printf("Playing playlist '%s' with %d songs\n", playlistName, len(songs))
		},
	}
}

func (c *CLI) peersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "peers",
		Short: "Manage peers",
	}

	cmd.AddCommand(c.peersListCommand())
	cmd.AddCommand(c.peersInfoCommand())
	cmd.AddCommand(c.peersConnectCommand())
	cmd.AddCommand(c.peersDisconnectCommand())

	return cmd
}

func (c *CLI) peersListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List connected peers",
		Run: func(cmd *cobra.Command, args []string) {
			peers := c.network.GetPeers()
			if len(peers) == 0 {
				log.Println("No peers connected")
				return
			}

			log.Printf("Connected peers (%d):\n", len(peers))
			for _, p := range peers {
				log.Printf("  - %s\n", p.ID)
			}
		},
	}
}

func (c *CLI) peersInfoCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show own peer info",
		Run: func(cmd *cobra.Command, args []string) {
			peerID := c.network.GetPeerID()
			multiaddr := c.network.GetMultiaddr()
			peerCount := c.network.GetPeerCount()
			online := c.network.IsOnline()

			status := "Offline"
			if online {
				status = "Online"
			}

			log.Printf("Your Peer ID: %s\n", peerID)
			log.Printf("Your Multiaddr: %s\n", multiaddr)
			log.Printf("Network Status: %s\n", status)
			log.Printf("Connected Peers: %d\n", peerCount)
		},
	}
}

func (c *CLI) peersConnectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "connect <multiaddr>",
		Short: "Connect to a peer by multiaddr",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			addr := args[0]

			addrInfo, err := peer.AddrInfoFromString(addr)
			if err != nil {
				log.Printf("Error: Invalid multiaddr: %v\n", err)
				return
			}

			if err := c.network.Connect(cmd.Context(), *addrInfo); err != nil {
				log.Printf("Error: %v\n", err)
				return
			}
			log.Printf("Connected to peer: %s\n", addrInfo.ID)
		},
	}
}

func (c *CLI) peersDisconnectCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect <peerID>",
		Short: "Disconnect from a peer",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			peerID, err := peer.Decode(args[0])
			if err != nil {
				log.Printf("Error: Invalid peer ID: %v\n", err)
				return
			}
			if err := c.network.Disconnect(peerID); err != nil {
				log.Printf("Error: %v\n", err)
				return
			}
			log.Printf("Disconnected from peer: %s\n", peerID)
		},
	}
}

func (c *CLI) Start() error {
	return c.rootCmd.Execute()
}

func (c *CLI) configCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
	}

	cmd.AddCommand(c.configShowCommand())
	cmd.AddCommand(c.configSetCommand())
	cmd.AddCommand(c.configResetCommand())

	return cmd
}

func (c *CLI) configShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		Run: func(cmd *cobra.Command, args []string) {
			if c.storage == nil {
				log.Println("Error: Storage not initialized")
				return
			}

			cfg, err := c.storage.LoadConfig()
			if err != nil {
				log.Printf("Error loading config: %v\n", err)
				return
			}

			log.Println("Current configuration:")
			log.Printf("  music_dir:   %s\n", cfg.MusicDir)
			log.Printf("  volume:      %d\n", cfg.Volume)
			log.Printf("  tui_enabled:%v\n", cfg.TUIEnabled)
			log.Printf("  wifi_mode:  %v\n", cfg.WifiMode)
			log.Printf("  offline:    %v\n", cfg.Offline)
			log.Printf("  rendezvous: %s\n", cfg.Rendezvous)
			log.Printf("  host:       %s\n", cfg.Host)
			log.Printf("  port:       %d\n", cfg.Port)
			log.Printf("  log_level:  %s\n", cfg.LogLevel)
		},
	}
}

func (c *CLI) configSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			if c.storage == nil {
				log.Println("Error: Storage not initialized")
				return
			}

			key := args[0]
			value := args[1]

			cfg, err := c.storage.LoadConfig()
			if err != nil {
				log.Printf("Error loading config: %v\n", err)
				return
			}

			switch key {
			case "musicdir":
				cfg.MusicDir = value
			case "volume":
				v, err := strconv.Atoi(value)
				if err != nil || v < 0 || v > 100 {
					log.Println("Error: Volume must be 0-100")
					return
				}
				cfg.Volume = v
			case "tui":
				v, err := strconv.ParseBool(value)
				if err != nil {
					log.Println("Error: tui must be true or false")
					return
				}
				cfg.TUIEnabled = v
			case "wifi":
				v, err := strconv.ParseBool(value)
				if err != nil {
					log.Println("Error: wifi must be true or false")
					return
				}
				cfg.WifiMode = v
			case "offline":
				v, err := strconv.ParseBool(value)
				if err != nil {
					log.Println("Error: offline must be true or false")
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
					log.Println("Error: Port must be 0-65535")
					return
				}
				cfg.Port = v
			case "loglevel":
				if value != "debug" && value != "info" && value != "warn" && value != "error" {
					log.Println("Error: log_level must be debug, info, warn, or error")
					return
				}
				cfg.LogLevel = value
			default:
				log.Printf("Error: Unknown key '%s'\n", key)
				log.Println("Available keys: musicdir, volume, tui, wifi, offline, rendezvous, host, port, loglevel")
				return
			}

			if err := c.storage.SaveConfig(cfg); err != nil {
				log.Printf("Error saving config: %v\n", err)
				return
			}
			log.Printf("Config updated: %s = %s\n", key, value)
		},
	}
}

func (c *CLI) configResetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "reset",
		Short: "Reset configuration to defaults",
		Run: func(cmd *cobra.Command, args []string) {
			if c.storage == nil {
				log.Println("Error: Storage not initialized")
				return
			}

			cfg := storage.DefaultConfig()
			if err := c.storage.SaveConfig(cfg); err != nil {
				log.Printf("Error saving config: %v\n", err)
				return
			}
			log.Println("Configuration reset to defaults")
		},
	}
}
