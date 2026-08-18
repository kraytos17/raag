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
		Use:   "remove [position]",
		Short: "Remove track at position from queue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			position, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("invalid position: %s", args[0])
			}
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.QueueRemove(int32(position))
			}), fmt.Sprintf("removed position %d from queue", position))
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
