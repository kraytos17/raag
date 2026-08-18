package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/p-society/raag/internal/infra/ipc"
	pb "github.com/p-society/raag/proto/gen"
	"github.com/spf13/cobra"
)

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
					fmt.Fprintf(os.Stdout, "volume: %d\n", status.Volume)
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

func newEqCmd() *cobra.Command {
	var eqOn, eqOff bool
	var eqBass, eqMid, eqTreble float64
	cmd := &cobra.Command{
		Use:   "eq",
		Short: "Get or set the equalizer",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("on") && !cmd.Flags().Changed("off") &&
				!cmd.Flags().Changed("bass") && !cmd.Flags().Changed("mid") && !cmd.Flags().Changed("treble") {
				resp, err := client.GetEqualizer()
				if err != nil {
					return err
				}
				if !resp.Success {
					return errors.New(resp.Error)
				}

				eq := resp.GetEqualizer()
				if eq == nil {
					return errors.New("no equalizer state returned")
				}
				fmt.Fprintf(os.Stdout, "enabled: %v\nbass: %+.1f dB\nmid: %+.1f dB\ntreble: %+.1f dB\n",
					eq.Enabled, eq.BassDb, eq.MidDb, eq.TrebleDb)
				return nil
			}

			enabled := false
			switch {
			case cmd.Flags().Changed("on") && eqOn:
				enabled = true
			case cmd.Flags().Changed("off") && eqOff:
				enabled = false
			default:
				if cmd.Flags().Changed("bass") || cmd.Flags().Changed("mid") || cmd.Flags().Changed("treble") {
					enabled = true
				}
			}
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.SetEqualizer(enabled, eqBass, eqMid, eqTreble)
			}), fmt.Sprintf("equalizer updated (enabled=%v bass=%+.1f mid=%+.1f treble=%+.1f)",
				enabled, eqBass, eqMid, eqTreble))
		},
	}

	cmd.Flags().BoolVar(&eqOn, "on", false, "enable the equalizer")
	cmd.Flags().BoolVar(&eqOff, "off", false, "disable the equalizer")
	cmd.Flags().Float64Var(&eqBass, "bass", 0, "bass gain in dB (-12..12)")
	cmd.Flags().Float64Var(&eqMid, "mid", 0, "mid gain in dB (-12..12)")
	cmd.Flags().Float64Var(&eqTreble, "treble", 0, "treble gain in dB (-12..12)")
	return cmd
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
				fmt.Fprintf(os.Stdout, "state: %s\nvolume: %d\nqueue: %d/%d\n",
					status.State, status.Volume, status.QueuePosition, status.QueueLength)
				if status.CurrentTrack != nil {
					fmt.Fprintf(os.Stdout, "track: %s — %s\n", status.CurrentTrack.Title, status.CurrentTrack.Artist)
				}
			}
			return nil
		},
	}
}
