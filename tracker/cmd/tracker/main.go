package main

import (
	"os"

	"github.com/p-society/raag/internal/logger"
	"github.com/p-society/raag/tracker"
	"github.com/spf13/cobra"
)

func main() {
	logger.NewLogger("raag::tracker")
	rootCmd := &cobra.Command{
		Use:   "tracker",
		Short: "Raag centralized tracker server",
		Run: func(cmd *cobra.Command, args []string) {
			port, _ := cmd.Flags().GetInt("port")
			tracker.StartServer(port)
		},
	}

	rootCmd.Flags().Int("port", 8080, "Tracker server port")
	if err := rootCmd.Execute(); err != nil {
		logger.Errorf("failed to execute command error=%v", err)
		os.Exit(1)
	}
}
