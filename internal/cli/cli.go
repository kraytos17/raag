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
	"github.com/spf13/cobra"
)

type CLI struct {
	library   *library.Library
	player    *player.Player
	network   *network.NetworkManager
	playlists *playlist.Manager
	rootCmd   *cobra.Command
}

func NewCLI(lib *library.Library, p *player.Player, net *network.NetworkManager, pm *playlist.Manager) *CLI {
	cli := &CLI{
		library:   lib,
		player:    p,
		network:   net,
		playlists: pm,
	}
	cli.rootCmd = &cobra.Command{
		Use:   "raag",
		Short: "Raag CLI for decentralized music streaming",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
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

func (c *CLI) Start() error {
	return c.rootCmd.Execute()
}
