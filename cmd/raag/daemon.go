package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/p-society/raag/app"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/internal/tui"
	"github.com/p-society/raag/rpc"
	"github.com/spf13/cobra"
)

func daemonCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run raag as a background daemon",
		Long: `Start the raag daemon in background mode.

The daemon maintains live peer connections and exposes a Unix socket
for fast CLI queries. Use 'raag peers list' and other commands to query.

Examples:
  raag daemon --network`,
		Run: func(cmd *cobra.Command, args []string) {
			runDaemon(cmd)
		},
	}
	return cmd
}

func runDaemon(cmd *cobra.Command) {
	logger.Infof("starting Raag daemon")
	if tuiEnabled, _ := cmd.Flags().GetBool("tui"); tuiEnabled {
		client := newRPCClient()
		if !client.IsAvailable() {
			logger.Errorf("Daemon not running. Start daemon first without --tui")
			os.Exit(1)
		}
		if err := tui.Start(client); err != nil {
			logger.Errorf("TUI error error=%v", err)
		}
		return
	}

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
	if networkFlag := cmd.Flags().Lookup("network"); networkFlag != nil && networkFlag.Value.String() == "true" {
		cfg.Network = true
	}
	if logLevel, _ := cmd.Flags().GetString("log-level"); logLevel == "" {
		logger.SetLevel(cfg.LogLevel)
	}

	a, err := app.NewApp(v, cfg)
	if err != nil {
		logger.Errorf("failed to create app error=%v", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a.StartNetwork(ctx)
	rpcServer := rpc.NewServer(config.SocketPath(), a, func() {
		cancel()
	})
	if err := rpcServer.Start(); err != nil {
		logger.Errorf("failed to start RPC server error=%v", err)
		os.Exit(1)
	}

	logger.Infof("Raag daemon started. Use Ctrl+C to stop.")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case <-ctx.Done():
		logger.Infof("received termination signal")
	case <-rpcServer.ShutdownRequested():
		logger.Infof("shutdown requested via RPC")
	}

	logger.Infof("shutting down daemon")
	rpcServer.Stop()
	if err := a.NetMgr.Close(); err != nil {
		logger.Warnf("failed to close network manager error=%v", err)
	}

	a.SaveState(context.Background())
	logger.Infof("daemon stopped")
}
