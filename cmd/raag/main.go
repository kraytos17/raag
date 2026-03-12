package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/constants"
	"github.com/p-society/raag/internal/library"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/network"
	"github.com/p-society/raag/internal/player"
	"github.com/p-society/raag/internal/playlist"
	"github.com/p-society/raag/internal/storage"
	"github.com/p-society/raag/internal/tui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	v         *viper.Viper
	cfg       *config.Config
	store     *storage.Storage
	lib       *library.Library
	p         *player.Player
	netMgr    *network.NetworkManager
	pm        *playlist.Manager
	ctx       context.Context
	cancelCtx context.CancelFunc
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "raag",
		Short: "Raag - Decentralized Music Streaming",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				if err := initializeApp(cmd); err != nil {
					logger.Errorf("Error initializing error=%v", err)
					return
				}
				if shouldStartTUI(cmd, cfg, false, false) {
					startTUI()
					return
				}
				logger.Infof("Starting peer discovery...")
				<-ctx.Done()
			}
		},
	}

	rootCmd.PersistentFlags().String("config", "", "config file (default is $HOME/.config/raag/config.yaml)")
	rootCmd.PersistentFlags().String("musicdir", "./music", "Directory containing music files")
	rootCmd.PersistentFlags().Bool("network", false, "Enable network mode for peer discovery")
	rootCmd.PersistentFlags().Bool("tui", false, "Start in TUI mode")
	rootCmd.PersistentFlags().String("tracker", "", "Centralized tracker URL for peer discovery")
	rootCmd.PersistentFlags().Int("port", constants.DefaultPort, "Node listen port (use 0 for random)")
	rootCmd.PersistentFlags().Bool("dht", true, "Enable DHT discovery")
	rootCmd.PersistentFlags().Int("max-peers", constants.DefaultMaxPeers, "Maximum number of peers to maintain")
	rootCmd.PersistentFlags().StringSlice("bootstrap", []string{}, "DHT bootstrap peers (multiaddr)")
	rootCmd.PersistentFlags().String("host", "127.0.0.1", "The host address to listen on")
	rootCmd.PersistentFlags().String("rendezvous", constants.DefaultRendezvous, "Unique string to identify Raag nodes")

	// Add subcommands
	rootCmd.AddCommand(playCommand())
	rootCmd.AddCommand(pauseCommand())
	rootCmd.AddCommand(resumeCommand())
	rootCmd.AddCommand(stopCommand())
	rootCmd.AddCommand(nextCommand())
	rootCmd.AddCommand(previousCommand())
	rootCmd.AddCommand(queueCommand())
	rootCmd.AddCommand(volumeCommand())
	rootCmd.AddCommand(seekCommand())
	rootCmd.AddCommand(nowplayingCommand())
	rootCmd.AddCommand(peersCommand())
	rootCmd.AddCommand(libraryCommand())
	rootCmd.AddCommand(playlistCommand())
	rootCmd.AddCommand(shareCommand())
	rootCmd.AddCommand(configCommand())
	rootCmd.AddCommand(daemonCommand())
	rootCmd.AddCommand(statusCommand())

	if err := rootCmd.Execute(); err != nil {
		logger.Errorf("Command execution failed error=%v", err)
	}
}

func initializeApp(cmd *cobra.Command) error {
	rt, err := bootstrapRuntime(cmd)
	if err != nil {
		return fmt.Errorf("bootstrap runtime: %w", err)
	}

	applyRuntime(rt)
	ctx, cancelCtx = context.WithCancel(context.Background())
	go func() {
		if err := netMgr.Start(ctx); err != nil {
			if err == context.Canceled {
				logger.Infof("Network stopped")
				return
			}
			logger.Errorf("Network error error=%v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		logger.Infof("Received termination signal, shutting down")
		shutdown()
		os.Exit(0)
	}()

	return nil
}

func startTUI() {
	if cfg == nil {
		logger.Errorf("Configuration not initialized")
		os.Exit(1)
	}
	if err := tui.Start(lib, p, netMgr, pm); err != nil {
		logger.Errorf("Error in TUI error=%v", err)
	}
	shutdown()
}

func shutdown() {
	if cancelCtx != nil {
		cancelCtx()
	}
	if store == nil {
		return
	}

	state := &storage.PlayerState{
		Volume: int(p.GetVolume()),
	}
	if song := p.GetCurrentSong(); song != nil {
		state.LastSong = song.Title
		state.Position = p.GetPosition()
	}
	if err := store.SaveState(state); err != nil {
		logger.Warnf("Could not save state error=%v", err)
	}
	if err := store.SavePlaylists(pm); err != nil {
		logger.Warnf("Could not save playlists error=%v", err)
	}
	if err := config.SaveConfig(v, cfg); err != nil {
		logger.Errorf("Error saving config error=%v", err)
	}
}
