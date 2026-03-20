package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/infra/audio"
	"github.com/p-society/raag/internal/infra/db"
	"github.com/p-society/raag/internal/infra/events"
	"github.com/p-society/raag/internal/infra/ipc"
	"github.com/p-society/raag/internal/infra/ipc/commands"
)

func main() {
	musicPath := flag.String("music-path", "", "Music directory path")
	socketPath := flag.String("socket", "", "IPC socket path (overrides config)")
	dataDir := flag.String("data-dir", "", "Data directory (overrides config)")
	noScan := flag.Bool("no-scan", false, "Skip library scan on startup")
	setup := flag.Bool("setup", false, "Run first-time setup wizard")
	flag.Usage = usage
	flag.Parse()

	if *setup {
		if err := config.RunSetup(); err != nil {
			slog.Error("setup failed", "error", err)
			os.Exit(1)
		}
		return
	}

	var cfg *config.Config
	var err error
	if *musicPath != "" {
		cfg, err = config.LoadWithMusicPath(*musicPath)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		slog.Error("failed to load config", "error", err)
		if isPathNotConfiguredError(err) {
			slog.Error("hint: run 'raagd --setup' for first-time setup")
			slog.Error("or specify --music-path: raagd --music-path ~/Music")
		}
		os.Exit(1)
	}
	if err := cfg.ValidateForStart(); err != nil {
		slog.Error("configuration invalid", "error", err)
		slog.Error("hint: run 'raagd --setup' for first-time setup")
		slog.Error("or specify --music-path: raagd --music-path ~/Music")
		os.Exit(1)
	}

	if *socketPath != "" {
		cfg.Daemon.SocketPath = config.ExpandHome(*socketPath)
	}
	if *dataDir != "" {
		cfg.Daemon.DataDir = config.ExpandHome(*dataDir)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel(cfg.Daemon.LogLevel),
	}))
	slog.SetDefault(logger)

	dbOpts := db.DefaultOptions(cfg.Daemon.DataDir)
	database, err := db.Open(cfg.Daemon.DataDir, dbOpts)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	bus := events.New()
	libraryRepo, err := db.NewLibraryRepo(database, cfg.Library.Paths)
	if err != nil {
		slog.Error("failed to create library repo", "error", err)
		os.Exit(1)
	}

	searchIndex := db.NewSearchIndex(database)
	peerRepo := db.NewPeerRepo(database)

	scanner := app.NewLibraryScanner(libraryRepo, searchIndex, bus, cfg.Library.Paths)
	searchService := app.NewSearchService(searchIndex, libraryRepo)

	resolver := app.NewResolver(libraryRepo)
	player := audio.NewEngine(cfg.Playback.SampleRate)
	queue := audio.NewQueue()

	playback := app.NewPlaybackController(libraryRepo, searchIndex, player, resolver, bus)
	cmdHandlers := commands.NewHandlers(playback, scanner, searchService, libraryRepo, peerRepo, queue)
	router := commands.NewRouter()
	cmdHandlers.RegisterAll(router)

	ipcServer := ipc.NewServer(cfg.Daemon.SocketPath, router)
	if err := ipcServer.Start(context.Background()); err != nil {
		slog.Error("failed to start IPC server", "error", err)
		os.Exit(1)
	}
	if cfg.Library.ScanOnStart && !*noScan {
		slog.Info("starting library scan", "paths", cfg.Library.Paths)
		go func() {
			if _, err := scanner.Scan(context.Background()); err != nil {
				slog.Error("library scan failed", "error", err)
			}
		}()
	}

	slog.Info("raag daemon started",
		"socket", cfg.Daemon.SocketPath,
		"data_dir", cfg.Daemon.DataDir,
		"volume", cfg.Playback.Volume,
	)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	slog.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*1e9)
	defer cancel()

	ipcServer.Stop(ctx)
	if err := player.Stop(ctx); err != nil {
		slog.Warn("player stop error", "error", err)
	}
	slog.Info("raag daemon stopped")
}

func usage() {
	fmt.Fprintf(os.Stderr, `Raag - Terminal music player with P2P streaming

Usage: raagd [flags]

Flags:
  --music-path path   Music directory path (required on first run)
  --socket path       IPC socket path (default ~/.local/share/raag/raag.sock)
  --data-dir path     Data directory (default ~/.local/share/raag)
  --no-scan           Skip library scan on startup
  --setup             Run first-time setup wizard
  -h, --help          Show this help

Examples:
  raagd --setup                    # First-time setup
  raagd --music-path ~/Music      # Run with specified music path
  raagd                            # Run with config file

`)
}

func isPathNotConfiguredError(err error) bool {
	return err != nil && (err.Error() == "no music path configured" ||
		err.Error() == "config validation failed: no music path configured")
}

func logLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
