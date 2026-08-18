package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/infra/ipc"
	"github.com/spf13/cobra"
)

// newDaemonCmd manages the raagd daemon process (start / stop / status).
func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the raagd daemon process",
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Start the daemon in the background",
			RunE:  runDaemonStart,
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the running daemon",
			RunE:  runDaemonStop,
		},
		&cobra.Command{
			Use:   "status",
			Short: "Report daemon status (exit 0 running, 1 stopped)",
			RunE:  runDaemonStatus,
		},
	)
	return cmd
}

// daemonConfig loads the daemon config paths used by the lifecycle commands.
func daemonConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return cfg, nil
}

// resolveDaemonBinary locates the raagd executable next to this raag binary,
// falling back to PATH.
func resolveDaemonBinary() (string, error) {
	if exe, err := os.Executable(); err == nil {
		if p, ok := findDaemonBinary(filepath.Dir(exe)); ok {
			return p, nil
		}
	}
	if p, err := exec.LookPath("raagd"); err == nil {
		return p, nil
	}
	return "", errors.New("raagd binary not found (build it with `make build-raagd` or add it to PATH)")
}

// findDaemonBinary returns the path to a raagd executable inside dir, if one
// exists there.
func findDaemonBinary(dir string) (string, bool) {
	candidate := filepath.Join(dir, "raagd")
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, true
	}
	return "", false
}

// waitForDaemon polls the IPC socket until the daemon answers or the timeout
// elapses.
func waitForDaemon(socketPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c := ipc.NewClient(socketPath)
		resp, err := c.HealthCheck()
		_ = c.Close()
		if err == nil && resp != nil && resp.Success {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("daemon did not become ready within %s", timeout)
}

func runDaemonStart(cmd *cobra.Command, _ []string) error {
	cfg, err := daemonConfig()
	if err != nil {
		return err
	}

	bin, err := resolveDaemonBinary()
	if err != nil {
		return err
	}
	if pid, alive := readDaemonPid(cfg.Daemon.PidFile); alive {
		return fmt.Errorf("daemon already running (pid %d)", pid)
	}

	logPath := cfg.Daemon.PidFile
	if logPath == "" {
		logPath = "raagd.log"
	} else {
		logPath = filepath.Join(filepath.Dir(cfg.Daemon.PidFile), "raagd.log")
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open daemon log: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	proc := exec.Command(bin)
	proc.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	proc.Stdout = logFile
	proc.Stderr = logFile
	if err := proc.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	socket := cfg.Daemon.SocketPath
	if err := waitForDaemon(socket, 5*time.Second); err != nil {
		_ = proc.Process.Kill()
		return err
	}
	fmt.Fprintf(os.Stdout, "daemon started (pid %d, socket %s)\n", proc.Process.Pid, socket)
	return nil
}

func runDaemonStop(cmd *cobra.Command, _ []string) error {
	cfg, err := daemonConfig()
	if err != nil {
		return err
	}

	pid, alive := readDaemonPid(cfg.Daemon.PidFile)
	if !alive {
		return errors.New("daemon is not running (no pid file)")
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find daemon process: %w", err)
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	_ = os.Remove(cfg.Daemon.PidFile)
	fmt.Fprintf(os.Stdout, "daemon stopped (pid %d)\n", pid)
	return nil
}

func runDaemonStatus(cmd *cobra.Command, _ []string) error {
	cfg, err := daemonConfig()
	if err != nil {
		return err
	}
	if pid, alive := readDaemonPid(cfg.Daemon.PidFile); alive {
		fmt.Fprintf(os.Stdout, "running (pid %d)\n", pid)
		return nil
	}
	if cfg.Daemon.SocketPath != "" {
		c := ipc.NewClient(cfg.Daemon.SocketPath)
		resp, err := c.HealthCheck()
		_ = c.Close()
		if err == nil && resp != nil && resp.Success {
			fmt.Fprintln(os.Stdout, "running (no pid file)")
			return nil
		}
	}
	fmt.Fprintln(os.Stdout, "stopped")
	return nil
}

// readDaemonPid returns the pid from the pid file and whether it's alive.
func readDaemonPid(path string) (int, bool) {
	if path == "" {
		return 0, false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return pid, false
	}
	return pid, true
}
