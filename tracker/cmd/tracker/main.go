package main

import (
	"os"

	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/tracker"
	"github.com/spf13/cobra"
)

func main() {
	logger.NewLogger("raag::tracker")

	var cfg struct {
		httpPort       int
		libp2pPort     int
		relayEnabled   bool
		dhtEnabled     bool
		authToken      string
		bootstrapPeers []string
	}

	rootCmd := &cobra.Command{
		Use:   "tracker",
		Short: "Raag centralized tracker server with libp2p relay and DHT",
		Long: `Raag Tracker serves as both:
- HTTP API for peer registration and discovery
- libp2p relay for NAT traversal
- DHT bootstrap node for peer discovery

Example:
  ./tracker --http-port 8080 --libp2p-port 45678 --relay --dht`,
		RunE: func(cmd *cobra.Command, args []string) error {
			config := tracker.TrackerConfig{
				HTTPPort:     cfg.httpPort,
				Libp2pPort:   cfg.libp2pPort,
				RelayEnabled: cfg.relayEnabled,
				DHTEnabled:   cfg.dhtEnabled,
			}

			t := tracker.NewTracker(config)
			if cfg.authToken != "" {
				logger.Infof("Authorization enabled - tokens required for peer registration")
			}
			return t.Start()
		},
	}

	rootCmd.Flags().IntVar(&cfg.httpPort, "http-port", constants.DefaultHTTPPort, "HTTP API listen port")
	rootCmd.Flags().IntVar(&cfg.libp2pPort, "libp2p-port", constants.DefaultPort, "libp2p listen port")
	rootCmd.Flags().BoolVar(&cfg.relayEnabled, "relay", true, "Enable circuit relay for NAT traversal")
	rootCmd.Flags().BoolVar(&cfg.dhtEnabled, "dht", true, "Enable DHT bootstrap node")
	rootCmd.Flags().StringVar(&cfg.authToken, "auth-token", "", "Optional token for peer registration auth")
	rootCmd.Flags().StringSliceVar(&cfg.bootstrapPeers, "bootstrap", []string{}, "DHT bootstrap peers (multiaddr)")

	if err := rootCmd.Execute(); err != nil {
		logger.Errorf("failed to execute command error=%v", err)
		os.Exit(1)
	}
}
