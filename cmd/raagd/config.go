package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/infra/observability"
	"github.com/p-society/raag/internal/version"
	"golang.org/x/term"
)

func loadConfig() (*config.Config, bool) {
	setup := flag.Bool("setup", false, "Run first-time setup wizard")
	musicPath := flag.String("music-path", "", "Music directory path")
	socketPath := flag.String("socket", "", "IPC socket path (overrides config)")
	dataDir := flag.String("data-dir", "", "Data directory (overrides config)")
	noScan := flag.Bool("no-scan", false, "Skip library scan on startup")
	p2pFlag := flag.Bool("p2p", false, "Enable P2P networking (use --p2p=false to disable)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Fprintln(os.Stdout, version.String("raagd"))
		os.Exit(0)
	}

	var cfg *config.Config
	var err error
	if *musicPath != "" {
		cfg, err = config.LoadWithMusicPath(*musicPath)
	} else {
		cfg, err = config.Load()
	}

	if *setup || err != nil || (cfg != nil && len(cfg.Library.Paths) == 0) {
		if !isInteractive() && !*setup {
			fmt.Fprintf(os.Stderr, "error: no config found and not running in interactive mode. Run with --setup to run the setup wizard.\n")
			os.Exit(1)
		}

		slog.Info("No configuration found. Running first-time setup...")
		if err := config.RunSetup(); err != nil {
			slog.Error("setup failed", "error", err)
			os.Exit(1)
		}

		cfg, err = config.Load()
		if err != nil {
			slog.Error("failed to load config after setup", "error", err)
			os.Exit(1)
		}
		if err := cfg.ValidateForStart(); err != nil {
			slog.Error("configuration still invalid after setup", "error", err)
			os.Exit(1)
		}
	}
	if *socketPath != "" {
		cfg.Daemon.SocketPath = config.ExpandHome(*socketPath)
	}
	if *dataDir != "" {
		cfg.Daemon.DataDir = config.ExpandHome(*dataDir)
	}

	p2pSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "p2p" {
			p2pSet = true
		}
	})
	if p2pSet {
		cfg.P2P.Enabled = *p2pFlag
	}

	slog.SetDefault(observability.NewLogger(observability.Config{
		Level:     observability.ParseLevel(cfg.Daemon.LogLevel),
		AddSource: true,
	}))
	return cfg, *noScan
}

func usage() {
	fmt.Fprintf(os.Stderr, `Raag - Terminal music player with P2P streaming

Usage: raagd [flags]

Flags:
  --music-path path   Music directory path (overrides config)
  --socket path       IPC socket path (default ~/.local/share/raag/raag.sock)
  --data-dir path     Data directory (default ~/.local/share/raag)
  --no-scan           Skip library scan on startup
  --p2p               Enable P2P networking (overrides config)
  -h, --help          Show this help

On First Run:
  Simply run 'raagd' and you'll be guided through setup automatically.

Examples:
  raagd                            # First run: automatic setup wizard
  raagd                            # Subsequent runs: start daemon
  raagd --music-path ~/Music      # Override music path

`)
}

func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
