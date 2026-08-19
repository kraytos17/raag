package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/infra/ipc"
	"github.com/p-society/raag/internal/version"
	pb "github.com/p-society/raag/proto/gen"
	"github.com/spf13/cobra"
)

var (
	socketPath string
	client     *ipc.Client
)

func main() {
	os.Exit(run())
}

func run() int {
	defer func() {
		if client != nil {
			client.Close()
		}
	}()

	rootCmd := &cobra.Command{
		Use:     "raag",
		Short:   "Raag - Terminal music player with P2P streaming",
		Version: version.String("raag"),
	}

	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.PersistentFlags().StringVar(&socketPath, "socket", "", "IPC socket path (default: ~/.local/share/raag/raag.sock)")

	rootCmd.AddCommand(
		newPlayCmd(),
		newPauseCmd(),
		newResumeCmd(),
		newStopCmd(),
		newNextCmd(),
		newPrevCmd(),
		newSeekCmd(),
		newVolumeCmd(),
		newEqCmd(),
		newStatusCmd(),
		newSearchCmd(),
		newQueueCmd(),
		newLibCmd(),
		newTrackCmd(),
		newHealthCmd(),
		newDaemonCmd(),
		newDebugCmd(),
		newPeersCmd(),
		newNetworkCmd(),
		newPlaylistCmd(),
		newTuiCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		return 1
	}
	return 0
}

func getClient() (*ipc.Client, error) {
	if client != nil {
		return client, nil
	}

	socket := socketPath
	if socket == "" {
		cfg, err := config.Load()
		if err != nil {
			return nil, fmt.Errorf("failed to load config: %w", err)
		}
		socket = config.ExpandHome(cfg.Daemon.SocketPath)
	}

	client = ipc.NewClient(socket)
	return client, nil
}

func call(fn func(*ipc.Client) (*pb.Response, error)) error {
	client, err := getClient()
	if err != nil {
		return err
	}

	resp, err := fn(client)
	if err != nil {
		return err
	}
	if !resp.Success {
		return errors.New(resp.Error)
	}
	return nil
}
