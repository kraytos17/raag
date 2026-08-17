package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/infra/ipc"
	"github.com/p-society/raag/internal/ui/tui"
	pb "github.com/p-society/raag/proto/gen"
	"github.com/spf13/cobra"
)

var (
	socketPath string
	client     *ipc.Client
)

func main() {
	os.Exit(run())
}

func run() int {
	defer func() {
		if client != nil {
			client.Close()
		}
	}()

	rootCmd := &cobra.Command{
		Use:   "raag",
		Short: "Raag - Terminal music player with P2P streaming",
	}

	rootCmd.PersistentFlags().StringVar(&socketPath, "socket", "", "IPC socket path (default: ~/.local/share/raag/raag.sock)")

	rootCmd.AddCommand(
		newPlayCmd(),
		newPauseCmd(),
		newResumeCmd(),
		newStopCmd(),
		newNextCmd(),
		newPrevCmd(),
		newSeekCmd(),
		newVolumeCmd(),
		newStatusCmd(),
		newSearchCmd(),
		newQueueCmd(),
		newLibCmd(),
		newPeersCmd(),
		newNetworkCmd(),
		newTuiCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		return 1
	}
	return 0
}

func getClient() (*ipc.Client, error) {
	if client != nil {
		return client, nil
	}

	socket := socketPath
	if socket == "" {
		cfg, err := config.Load()
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		socket = config.ExpandHome(cfg.Daemon.SocketPath)
	}

	client = ipc.NewClient(socket)
	return client, nil
}

func call(fn func(*ipc.Client) (*pb.Response, error)) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	resp, err := fn(client)
	if err != nil {
		return err
	}
	if !resp.Success {
		return errors.New(resp.Error)
	}
	return nil
}

func newPlayCmd() *cobra.Command {
	var byID bool
	cmd := &cobra.Command{
		Use:   "play [query]",
		Short: "Play a track by search query or track ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			trackID, q := "", ""
			if byID {
				trackID = query
			} else {
				q = query
			}
			if err := call(func(c *ipc.Client) (*pb.Response, error) {
				return c.Play(trackID, q)
			}); err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, "Playing")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&byID, "id", "i", false, "Treat argument as track ID instead of search query")
	return cmd
}

func newPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause playback",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.Pause()
			}), "paused")
		},
	}
}

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume playback",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.Resume()
			}), "resumed")
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop playback",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.Stop()
			}), "stopped")
		},
	}
}

func newNextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "next",
		Short: "Skip to next track",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.Next()
			}), "next track")
		},
	}
}

func newPrevCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prev",
		Short: "Go to previous track",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.Prev()
			}), "previous track")
		},
	}
}

func withSuccess(err error, msg string) error {
	if err == nil {
		fmt.Fprintln(os.Stdout, msg)
	}
	return err
}

func newSeekCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seek [seconds]",
		Short: "Seek to position in seconds",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			seconds, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid seconds: %s", args[0])
			}

			client, err := getClient()
			if err != nil {
				return err
			}
			return withSuccess(client.SeekTo(int64(seconds)*1000), fmt.Sprintf("seeked %ds", seconds))
		},
	}
}

func newVolumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "volume [0-100]",
		Short: "Get or set volume",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				resp, err := client.Status()
				if err != nil {
					return err
				}
				if !resp.Success {
					return errors.New(resp.Error)
				}
				if status := resp.GetStatus(); status != nil {
					slog.Info("volume", "level", status.Volume)
				}
				return nil
			}

			var volume int
			volume, err = strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid volume: %s", args[0])
			}
			if volume < 0 || volume > 100 {
				return fmt.Errorf("volume must be 0-100")
			}
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.SetVolume(int32(volume))
			}), fmt.Sprintf("volume set to %d", volume))
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show playback status",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			resp, err := client.Status()
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}
			if status := resp.GetStatus(); status != nil {
				slog.Info("status", "state", status.State, "volume", status.Volume, "queue", status.QueueLength)
				if status.CurrentTrack != nil {
					slog.Info("current track", "title", status.CurrentTrack.Title, "artist", status.CurrentTrack.Artist)
				}
			}
			return nil
		},
	}
}

func newSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search [query]",
		Short: "Search library for tracks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			resp, err := client.Search(args[0], 20)
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}
			if searchResp := resp.GetSearch(); searchResp != nil {
				if len(searchResp.Tracks) == 0 {
					slog.Info("no tracks found")
					return nil
				}
				for i, track := range searchResp.Tracks {
					slog.Info("track", "index", i+1, "title", track.Title, "artist", track.Artist, "album", track.Album)
				}
			}
			return nil
		},
	}
}

func newQueueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Manage playback queue",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "add [track-id]",
		Short: "Add track to queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.QueueAdd(args[0], -1)
			}), "added to queue")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Clear queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.QueueClear()
			}), "queue cleared")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "shuffle [on|off]",
		Short: "Toggle queue shuffle",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shuffle := true
			if len(args) == 1 {
				switch args[0] {
				case "on", "true":
					shuffle = true
				case "off", "false":
					shuffle = false
				default:
					return fmt.Errorf("invalid shuffle value %q (want on|off)", args[0])
				}
			}
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.QueueSetShuffle(shuffle)
			}), "shuffle updated")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "repeat [all|one|none]",
		Short: "Set queue repeat mode",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := "all"
			if len(args) == 1 {
				mode = args[0]
			}
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.QueueSetRepeat(mode)
			}), "repeat updated")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "mode",
		Short: "Show queue shuffle/repeat state",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			resp, err := client.QueueGetMode()
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}
			if qm := resp.GetQueueMode(); qm != nil {
				fmt.Fprintf(os.Stdout, "shuffle: %v\nrepeat: %q\n", qm.Shuffle, qm.Repeat)
			}
			return nil
		},
	})
	return cmd
}

func newLibCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lib",
		Short: "Library management",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "scan",
		Short: "Scan library for new tracks",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			_, err = client.LibScanAsync(false, func(progress ipc.ScanProgress) {
				if progress.Total > 0 {
					fmt.Printf("\rScanning: %d/%d files (%s)", progress.Scanned, progress.Total, progress.CurrentFile)
				}
			})
			if err != nil {
				return err
			}

			fmt.Println("\nLibrary scan complete")
			return nil
		},
	})
	return cmd
}

func newPeersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "peers",
		Short: "Manage P2P peers",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List connected peers",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			resp, err := client.ListPeers()
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}

			peersResp := resp.GetListPeers()
			if peersResp == nil {
				fmt.Fprintln(os.Stdout, "No peers connected")
				return nil
			}

			peers := peersResp.Peers
			if len(peers) == 0 {
				fmt.Fprintln(os.Stdout, "No peers connected")
				return nil
			}

			fmt.Fprintf(os.Stdout, "Connected peers (%d):\n", len(peers))
			for _, peer := range peers {
				fmt.Fprintf(os.Stdout, "  - %s", peer.Id)
				if peer.Capabilities != nil {
					fmt.Fprintf(os.Stdout, " (codecs: %v)", peer.Capabilities.SupportedCodecs)
				}
				fmt.Fprintln(os.Stdout)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "ban [peer-id]",
		Short: "Ban a peer by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.BanPeer(args[0])
			}), "peer banned")
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "unban [peer-id]",
		Short: "Unban a peer by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.UnbanPeer(args[0])
			}), "peer unbanned")
		},
	})
	return cmd
}

func newNetworkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Show P2P network status",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			resp, err := client.NetworkStatus()
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}

			netStatus := resp.GetNetworkStatus()
			if netStatus == nil {
				fmt.Fprintln(os.Stdout, "P2P is not enabled")
				return nil
			}

			fmt.Fprintln(os.Stdout, "=== Network Status ===")
			fmt.Fprintf(os.Stdout, "Self Peer ID: %s\n", netStatus.PeerId)
			fmt.Fprintf(os.Stdout, "Listen Addresses:\n")
			for _, addr := range netStatus.ListenAddrs {
				fmt.Fprintf(os.Stdout, "  - %s\n", addr)
			}

			fmt.Fprintf(os.Stdout, "Connected Peers: %d\n", len(netStatus.ConnectedPeers))
			for _, peer := range netStatus.ConnectedPeers {
				fmt.Fprintf(os.Stdout, "  - %s\n", peer.PeerId)
				for _, addr := range peer.Addrs {
					fmt.Fprintf(os.Stdout, "      %s\n", addr)
				}
			}

			fmt.Fprintf(os.Stdout, "Discovered Peers (mDNS): %d\n", len(netStatus.DiscoveredPeers))
			for _, peer := range netStatus.DiscoveredPeers {
				suffix := ""
				if peer.Dialable {
					suffix = " (dialable)"
				}

				fmt.Fprintf(os.Stdout, "  - %s%s\n", peer.PeerId, suffix)
				for _, addr := range peer.Addrs {
					fmt.Fprintf(os.Stdout, "      %s\n", addr)
				}
			}
			return nil
		},
	}
	return cmd
}

func newTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch interactive TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			socket := socketPath
			if socket == "" {
				cfg, err := config.Load()
				if err != nil {
					return fmt.Errorf("failed to load config: %w", err)
				}
				socket = config.ExpandHome(cfg.Daemon.SocketPath)
			}
			return tui.Run(socket)
		},
	}
}
