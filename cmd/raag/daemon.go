package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/library"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/network"
	"github.com/p-society/raag/internal/player"
	"github.com/p-society/raag/internal/playlist"
	"github.com/p-society/raag/internal/storage"
	"github.com/p-society/raag/internal/tui"
	"github.com/spf13/cobra"
)

var (
	daemonTrackerURL string
	daemonTUI        bool
	daemonNoTUI      bool
)

func daemonCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run raag as a background daemon",
		Long: `Start the raag daemon in background mode.
		
The daemon maintains live peer connections and exposes a Unix socket
for fast CLI queries. Use 'raag peers list' and other commands to query.

Examples:
  raag daemon --tracker http://localhost:8080
  raag daemon --tracker http://localhost:8080 --no-tui`,
		Run: func(cmd *cobra.Command, args []string) {
			runDaemon(cmd)
		},
	}

	cmd.Flags().StringVar(&daemonTrackerURL, "tracker", "", "Tracker URL")
	cmd.Flags().BoolVar(&daemonTUI, "tui", false, "Show TUI when running daemon")
	cmd.Flags().BoolVar(&daemonNoTUI, "no-tui", false, "Run without TUI (headless mode)")
	return cmd
}

func runDaemon(cmd *cobra.Command) {
	logger.Info("starting Raag daemon")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	v, err := config.InitViper(cmd)
	if err != nil {
		logger.Error("failed to initialize config", "error", err)
		os.Exit(1)
	}

	cfg, err := config.LoadConfig(v)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	if daemonTrackerURL != "" {
		cfg.TrackerURL = daemonTrackerURL
	}

	store, err := storage.New()
	if err != nil {
		logger.Warn("could not initialize storage", "error", err)
	}

	lib, err := library.NewLibrary(cfg.MusicDir)
	if err != nil {
		logger.Error("failed to initialize library", "error", err)
		os.Exit(1)
	}

	p, err := player.NewPlayer()
	if err != nil {
		logger.Error("failed to initialize player", "error", err)
		os.Exit(1)
	}
	p.SetVolume(float64(cfg.Volume))

	netMgr, err := network.NewNetwork(cfg, lib, cfg.MusicDir)
	if err != nil {
		logger.Error("failed to initialize network", "error", err)
		os.Exit(1)
	}

	pm = playlist.NewManager()
	if store != nil {
		if err := store.LoadPlaylists(pm); err != nil {
			logger.Warn("could not load playlists", "error", err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := netMgr.Start(ctx); err != nil {
			logger.Error("network error", "error", err)
		}
	}()

	socketServer := NewSocketServer(netMgr)
	if err := socketServer.Start(); err != nil {
		logger.Error("failed to start socket server", "error", err)
		os.Exit(1)
	}

	logger.Info("Raag daemon started. Use Ctrl+C to stop.")
	showTUI := daemonTUI && !daemonNoTUI
	if showTUI {
		go func() {
			tui.Start(lib, p, netMgr, pm)
			cancel()
		}()
	}

	<-sigCh
	logger.Info("shutting down daemon")

	socketServer.Stop()
	cancel()

	time.Sleep(1 * time.Second)
	logger.Info("daemon stopped")
}
