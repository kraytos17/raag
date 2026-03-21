package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/infra/ipc"
	pb "github.com/p-society/raag/proto/gen"
	"github.com/spf13/cobra"
)

var socketPath string

func main() {
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
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func getClient() (*ipc.Client, func(), error) {
	socket := socketPath
	if socket == "" {
		cfg, err := config.Load()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load config: %w", err)
		}
		socket = config.ExpandHome(cfg.Daemon.SocketPath)
	}

	client := ipc.NewClient(socket)
	return client, func() {}, nil
}

func call(fn func(*ipc.Client) (*pb.Response, error)) error {
	client, cleanup, err := getClient()
	if err != nil {
		return err
	}
	defer cleanup()

	resp, err := fn(client)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s", resp.Error)
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
			slog.Info("playing")
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
		slog.Info(msg)
	}
	return err
}

func newSeekCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seek [seconds]",
		Short: "Seek to position in seconds",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var seconds int
			if _, err := fmt.Sscanf(args[0], "%d", &seconds); err != nil {
				return fmt.Errorf("invalid seconds: %s", args[0])
			}

			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

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
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			if len(args) == 0 {
				resp, err := client.Status()
				if err != nil {
					return err
				}
				if !resp.Success {
					return fmt.Errorf("%s", resp.Error)
				}
				if status := resp.GetStatus(); status != nil {
					slog.Info("volume", "level", status.Volume)
				}
				return nil
			}

			var volume int
			if _, err := fmt.Sscanf(args[0], "%d", &volume); err != nil {
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
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := client.Status()
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
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
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := client.Search(args[0], 20)
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
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
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.LibScan(false)
			}), "library scan complete")
		},
	})
	return cmd
}
