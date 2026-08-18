package main

import (
	"fmt"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/ui/tui"
	"github.com/spf13/cobra"
)

func newTuiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch interactive TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			socket := socketPath
			if socket == "" {
				cfg, err := config.Load()
				if err != nil {
					return fmt.Errorf("failed to load config: %w", err)
				}
				socket = config.ExpandHome(cfg.Daemon.SocketPath)
			}
			return tui.Run(socket)
		},
	}
}
