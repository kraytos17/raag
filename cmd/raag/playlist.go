package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/p-society/raag/internal/infra/ipc"
	pb "github.com/p-society/raag/proto/gen"
	"github.com/spf13/cobra"
)

func newPlaylistCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "playlist",
		Short: "Manage playlists",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "create [name]",
		Short: "Create a playlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			resp, err := client.CreatePlaylist(args[0])
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}

			cp := resp.GetCreatePlaylist()
			if cp != nil {
				fmt.Fprintf(os.Stdout, "created playlist %s (%s)\n", cp.Name, cp.PlaylistId)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list", //nolint:goconst // cobra command names are string literals
		Short: "List playlists",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			resp, err := client.ListPlaylists()
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}

			playlists := resp.GetListPlaylists().Playlists
			if len(playlists) == 0 {
				fmt.Fprintln(os.Stdout, "no playlists")
				return nil
			}
			for _, p := range playlists {
				fmt.Fprintf(os.Stdout, "%s  %s  (%d tracks)\n", p.Id, p.Name, len(p.TrackIds))
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "show [playlist-id]",
		Short: "Show a playlist with its tracks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			resp, err := client.GetPlaylist(args[0])
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}

			gp := resp.GetGetPlaylist()
			if gp == nil || gp.Playlist == nil {
				return nil
			}

			fmt.Fprintf(os.Stdout, "Playlist: %s (%s)\n", gp.Playlist.Name, gp.Playlist.Id)
			for i, t := range gp.Tracks {
				fmt.Fprintf(os.Stdout, "  %d. %s - %s\n", i+1, t.Title, t.Artist)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add [playlist-id] [track-id]",
		Short: "Add a track to a playlist",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withSuccess(call(func(c *ipc.Client) (*pb.Response, error) {
				return c.AddToPlaylist(args[0], args[1])
			}), "added to playlist")
		},
	})
	return cmd
}
