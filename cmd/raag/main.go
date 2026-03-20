package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/ipc"
	"github.com/p-society/raag/internal/infra/ipc/commands"
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to connect to daemon: %w (is raagd running?)", err)
	}
	return client, func() { client.Close() }, nil
}

func sendCommand(cmd commands.Command) (commands.Response, error) {
	client, cleanup, err := getClient()
	if err != nil {
		return commands.Response{}, err
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return client.Send(ctx, cmd)
}

func newPlayCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "play [query or track-id]",
		Short: "Play a track by search query or track ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			var payload []byte
			if len(query) == 64 {
				payload, _ = json.Marshal(map[string]string{"track_id": query})
			} else {
				payload, _ = json.Marshal(map[string]string{"query": query})
			}

			resp, err := sendCommand(commands.Command{
				Type:    commands.CmdPlay,
				Payload: payload,
			})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
				return fmt.Errorf("%s", resp.Error)
			}

			slog.Info("playing")
			return nil
		},
	}
}

func newPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause",
		Short: "Pause playback",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := sendCommand(commands.Command{Type: commands.CmdPause})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
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
			resp, err := sendCommand(commands.Command{Type: commands.CmdResume})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
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
			resp, err := sendCommand(commands.Command{Type: commands.CmdStop})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
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
			resp, err := sendCommand(commands.Command{Type: commands.CmdNext})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
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
			resp, err := sendCommand(commands.Command{Type: commands.CmdPrev})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
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

			payload, _ := json.Marshal(map[string]int64{"offset_ms": int64(seconds) * 1000})
			resp, err := sendCommand(commands.Command{
				Type:    commands.CmdSeekTo,
				Payload: payload,
			})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
				return fmt.Errorf("%s", resp.Error)
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
			if len(args) == 0 {
				resp, err := sendCommand(commands.Command{Type: commands.CmdStatus})
				if err != nil {
					return err
				}
				if resp.Status != "ok" {
					return fmt.Errorf("%s", resp.Error)
				}
				var status struct {
					Volume int `json:"volume"`
				}

				json.Unmarshal(resp.Data, &status)
				slog.Info("volume", "level", status.Volume)
				return nil
			}

			var volume int
			if _, err := fmt.Sscanf(args[0], "%d", &volume); err != nil {
				return fmt.Errorf("invalid volume: %s", args[0])
			}
			if volume < 0 || volume > 100 {
				return fmt.Errorf("volume must be 0-100")
			}

			payload, _ := json.Marshal(map[string]int{"volume": volume})
			resp, err := sendCommand(commands.Command{
				Type:    commands.CmdSetVolume,
				Payload: payload,
			})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
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
			resp, err := sendCommand(commands.Command{Type: commands.CmdStatus})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
				return fmt.Errorf("%s", resp.Error)
			}

			var status struct {
				State        string        `json:"state"`
				CurrentTrack *domain.Track `json:"current_track"`
				Volume       int           `json:"volume"`
				QueueSize    int           `json:"queue_size"`
				QueuePos     int           `json:"queue_position"`
			}
			if err := json.Unmarshal(resp.Data, &status); err != nil {
				return err
			}

			slog.Info("status", "state", status.State, "volume", status.Volume, "queue", status.QueueSize)
			if status.CurrentTrack != nil {
				slog.Info("current track", "title", status.CurrentTrack.Title, "artist", status.CurrentTrack.Artist)
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
			payload, _ := json.Marshal(map[string]any{
				"query": args[0],
				"limit": 20,
			})

			resp, err := sendCommand(commands.Command{
				Type:    commands.CmdSearch,
				Payload: payload,
			})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
				return fmt.Errorf("%s", resp.Error)
			}

			var tracks []*domain.Track
			if err := json.Unmarshal(resp.Data, &tracks); err != nil {
				return err
			}
			if len(tracks) == 0 {
				slog.Info("no tracks found")
				return nil
			}
			for i, track := range tracks {
				slog.Info("track", "index", i+1, "title", track.Title, "artist", track.Artist, "album", track.Album)
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
			payload, _ := json.Marshal(map[string]any{"track_id": args[0]})
			resp, err := sendCommand(commands.Command{
				Type:    commands.CmdQueueAdd,
				Payload: payload,
			})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
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
			resp, err := sendCommand(commands.Command{Type: commands.CmdQueueClear})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
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
			resp, err := sendCommand(commands.Command{Type: commands.CmdLibScan})
			if err != nil {
				return err
			}
			if resp.Status != "ok" {
				return fmt.Errorf("%s", resp.Error)
			}
			slog.Info("library scan complete")
			return nil
		},
	})
	return cmd
}
