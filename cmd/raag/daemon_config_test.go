package main

import (
	"path/filepath"
	"testing"
)

func TestDaemonConfig_SocketOverride(t *testing.T) {
	old := socketPath
	defer func() { socketPath = old }()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	socketPath = filepath.Join(t.TempDir(), "override.sock")
	cfg, err := daemonConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Daemon.SocketPath != socketPath {
		t.Fatalf("SocketPath = %q, want override %q", cfg.Daemon.SocketPath, socketPath)
	}
}

func TestDaemonConfig_NoOverride(t *testing.T) {
	old := socketPath
	defer func() { socketPath = old }()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	socketPath = ""
	cfg, err := daemonConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Daemon.SocketPath == "" {
		t.Fatal("SocketPath empty; expected a default")
	}
}
