package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/infra/ipc"
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

func newPlayCmd() *cobra.Command {
	var byID bool
	cmd := &cobra.Command{
		Use:   "play [query]",
		Short: "Play a track by search query or track ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			trackID, q := "", ""
			if byID {
				trackID = query
			} else {
				q = query
			}
			resp, err := client.Play(trackID, q)
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
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
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := client.Pause()
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
			}

			slog.Info("paused")
			return nil
		},
	}
}

func newResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume",
		Short: "Resume playback",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := client.Resume()
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
			}

			slog.Info("resumed")
			return nil
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop playback",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := client.Stop()
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
			}

			slog.Info("stopped")
			return nil
		},
	}
}

func newNextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "next",
		Short: "Skip to next track",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := client.Next()
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
			}

			slog.Info("next track")
			return nil
		},
	}
}

func newPrevCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prev",
		Short: "Go to previous track",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := client.Prev()
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
			}

			slog.Info("previous track")
			return nil
		},
	}
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

			err = client.SeekTo(int64(seconds) * 1000)
			if err != nil {
				return err
			}

			slog.Info("seeked", "seconds", seconds)
			return nil
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

			resp, err := client.SetVolume(int32(volume))
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
			}

			slog.Info("volume set", "level", volume)
			return nil
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
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := client.QueueAdd(args[0], -1)
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
			}

			slog.Info("added to queue")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Clear queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := client.QueueClear()
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
			}

			slog.Info("queue cleared")
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
			client, cleanup, err := getClient()
			if err != nil {
				return err
			}
			defer cleanup()

			resp, err := client.LibScan(false)
			if err != nil {
				return err
			}
			if !resp.Success {
				return fmt.Errorf("%s", resp.Error)
			}

			slog.Info("library scan complete")
			return nil
		},
	})
	return cmd
}
