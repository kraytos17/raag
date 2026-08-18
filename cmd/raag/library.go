package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/p-society/raag/internal/infra/ipc"
	pb "github.com/p-society/raag/proto/gen"
	"github.com/spf13/cobra"
)

func newSearchCmd() *cobra.Command {
	var includePeers bool
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search library for tracks",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var resp *pb.Response
			if includePeers {
				resp, err = client.SearchRemote(args[0], 20)
			} else {
				resp, err = client.Search(args[0], 20)
			}

			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}
			if searchResp := resp.GetSearch(); searchResp != nil {
				if len(searchResp.Tracks) == 0 {
					fmt.Fprintln(os.Stdout, "no tracks found")
					return nil
				}
				for i, track := range searchResp.Tracks {
					line := fmt.Sprintf("%d. %s — %s (%s)", i+1, track.Title, track.Artist, track.Album)
					if track.PeerId != "" {
						line += fmt.Sprintf(" [peer %s]", track.PeerId)
					}
					fmt.Fprintln(os.Stdout, line)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&includePeers, "peers", false, "also search connected peers' libraries")
	return cmd
}

func newLibCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lib",
		Short: "Library management",
	}

	var scanIncremental bool
	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan library for new tracks",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			_, err = client.LibScanAsync(scanIncremental, func(progress ipc.ScanProgress) {
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
	}
	scanCmd.Flags().BoolVar(&scanIncremental, "incremental", false, "only scan new or modified files")
	cmd.AddCommand(scanCmd)

	var libListLimit int32
	listCmd := &cobra.Command{
		Use:   "list", //nolint:goconst // cobra command names are string literals
		Short: "List tracks in the library",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			resp, err := client.ListTracks(0, libListLimit)
			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}

			lt := resp.GetListTracks()
			if lt == nil {
				return nil
			}
			for _, t := range lt.Tracks {
				fmt.Fprintf(os.Stdout, "%s\t%s - %s\n", t.Id, t.Title, t.Artist)
			}
			return nil
		},
	}

	listCmd.Flags().Int32Var(&libListLimit, "limit", 0, "maximum number of tracks to list (0 = all)")
	cmd.AddCommand(listCmd)
	return cmd
}

func newTrackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "track",
		Short: "Inspect a library track",
	}

	var byPath string
	getCmd := &cobra.Command{
		Use:   "get [track-id]",
		Short: "Show a track's details",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getClient()
			if err != nil {
				return err
			}

			var resp *pb.Response
			switch {
			case byPath != "":
				resp, err = client.GetTrackByPath(byPath)
			case len(args) == 1:
				resp, err = client.GetTrack(args[0])
			default:
				return fmt.Errorf("provide a track-id or --path")
			}

			if err != nil {
				return err
			}
			if !resp.Success {
				return errors.New(resp.Error)
			}

			gt := resp.GetGetTrack()
			if gt == nil || gt.Track == nil {
				return errors.New("no track returned")
			}

			t := gt.Track
			fmt.Fprintf(os.Stdout, "ID:       %s\nTitle:    %s\nArtist:   %s\nAlbum:    %s\nDuration: %dms\n",
				t.Id, t.Title, t.Artist, t.Album, t.DurationMs)
			return nil
		},
	}

	getCmd.Flags().StringVar(&byPath, "path", "", "look up a track by file path")
	cmd.AddCommand(getCmd)
	return cmd
}
