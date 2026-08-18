package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/p-society/raag/internal/infra/ipc"
	pb "github.com/p-society/raag/proto/gen"
	"github.com/spf13/cobra"
)

func newPeersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "peers",
		Short: "Manage P2P peers",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list", //nolint:goconst // cobra command names are string literals
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
