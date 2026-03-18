package main

import (
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/rpc"
	"github.com/spf13/cobra"
)

func statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		Run: func(cmd *cobra.Command, args []string) {
			var result rpc.Status
			invokeRPC("DaemonService.Status", &rpc.EmptyArgs{}, &result)

			logger.Infof("=== Raag Daemon ===")
			logger.Infof("Running: %v", result.Running)
			logger.Infof("Uptime: %s", result.Uptime)
			logger.Infof("Version: %s", result.Version)
			logger.Infof("")
			logger.Infof("=== Network ===")
			logger.Infof("Online: %v", result.Connected)
			logger.Infof("Peers: %d", result.PeerCount)
		},
	}
}

func networkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "network",
		Short: "Manage network and P2P connections",
	}

	cmd.AddCommand(networkStatusCommand())
	return cmd
}

func networkStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show P2P network status",
		Run: func(cmd *cobra.Command, args []string) {
			var result rpc.NetworkStatusResult
			invokeRPC("DaemonService.NetworkStatus", &rpc.EmptyArgs{}, &result)

			logger.Infof("=== P2P Network Status ===")
			logger.Infof("Peer ID: %s", result.State.SelfID)
			logger.Infof("Listen Addr: %s", result.State.ListenAddr)
			logger.Infof("Mode: %s", result.State.Mode)
			logger.Infof("DHT: enabled=%v peers=%d", result.State.DHTEnabled, result.State.DHTPeers)
			logger.Infof("mDNS: enabled=%v discovered=%d", result.State.MDNSEnabled, result.State.MDNSDiscovered)
			logger.Infof("")
			logger.Infof("=== Connections ===")
			logger.Infof("Connected: %d", len(result.State.ConnectedPeers))
			logger.Infof("Known: %d", len(result.State.KnownPeers))
			if len(result.State.ConnectedPeers) > 0 {
				logger.Infof("")
				logger.Infof("Connected Peers:")
				for _, p := range result.State.ConnectedPeers {
					logger.Infof("  %s", p.ID)
				}
			}
		},
	}
}
