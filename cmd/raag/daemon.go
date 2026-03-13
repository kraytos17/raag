package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/tui"
	"github.com/spf13/cobra"
)

var daemonTrackerURL string

func daemonCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run raag as a background daemon",
		Long: `Start the raag daemon in background mode.

The daemon maintains live peer connections and exposes a Unix socket
for fast CLI queries. Use 'raag peers list' and other commands to query.

Examples:
  raag daemon --tracker http://localhost:8080
  raag daemon --tracker http://raag-production.up.railway.app`,
		Run: func(cmd *cobra.Command, args []string) {
			runDaemon(cmd)
		},
	}

	cmd.Flags().StringVar(&daemonTrackerURL, "tracker", "", "Tracker URL")
	return cmd
}

func runDaemon(cmd *cobra.Command) {
	logger.Infof("starting Raag daemon")
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	v, err := config.InitViper(cmd)
	if err != nil {
		logger.Errorf("failed to initialize config error=%v", err)
		os.Exit(1)
	}

	cfg, err := config.LoadConfig(v)
	if err != nil {
		logger.Errorf("failed to load config error=%v", err)
		os.Exit(1)
	}
	if daemonTrackerURL != "" {
		cfg.TrackerURL = daemonTrackerURL
		if err := config.SaveConfig(v, cfg); err != nil {
			logger.Warnf("failed to save tracker URL to config error=%v", err)
		} else {
			logger.Infof("saved tracker URL to config url=%s", daemonTrackerURL)
		}
	}

	logger.SetLevel(cfg.LogLevel)
	rt, err := bootstrapRuntime(cmd)
	if err != nil {
		logger.Errorf("failed to bootstrap runtime error=%v", err)
		os.Exit(1)
	}

	applyRuntime(rt)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := netMgr.Start(ctx); err != nil {
			if err == context.Canceled {
				logger.Infof("network stopped")
				return
			}
			logger.Errorf("network error error=%v", err)
		}
	}()

	socketServer := NewSocketServer(netMgr)
	if err := socketServer.Start(); err != nil {
		logger.Errorf("failed to start socket server error=%v", err)
		os.Exit(1)
	}

	logger.Infof("Raag daemon started. Use Ctrl+C to stop.")
	showTUI := shouldStartTUI(cmd)
	if showTUI {
		go func() {
			tui.Start(lib, p, netMgr, pm)
			cancel()
		}()
	}

	<-sigCh
	logger.Infof("shutting down daemon")

	socketServer.Stop()
	cancel()
	if netMgr != nil {
		if err := netMgr.Close(); err != nil {
			logger.Warnf("failed to close network manager error=%v", err)
		}
	}

	time.Sleep(1 * time.Second)
	logger.Infof("daemon stopped")
}
