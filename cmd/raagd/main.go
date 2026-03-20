package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/infra/audio"
	"github.com/p-society/raag/internal/infra/db"
	"github.com/p-society/raag/internal/infra/events"
	"github.com/p-society/raag/internal/infra/ipc"
)

func main() {
	musicPath := flag.String("music-path", "", "Music directory path")
	socketPath := flag.String("socket", "", "IPC socket path (overrides config)")
	dataDir := flag.String("data-dir", "", "Data directory (overrides config)")
	noScan := flag.Bool("no-scan", false, "Skip library scan on startup")
	flag.Usage = usage
	flag.Parse()

	var cfg *config.Config
	var err error
	if *musicPath != "" {
		cfg, err = config.LoadWithMusicPath(*musicPath)
	} else {
		cfg, err = config.Load()
	}
	if err != nil {
		if isPathNotConfiguredError(err) {
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
		} else {
			slog.Error("failed to load config", "error", err)
			os.Exit(1)
		}
	}
	if err := cfg.ValidateForStart(); err != nil {
		if isPathNotConfiguredError(err) {
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
		} else {
			slog.Error("configuration invalid", "error", err)
			slog.Error("hint: run 'raagd --setup' to reconfigure")
			slog.Error("or specify --music-path: raagd --music-path ~/Music")
			os.Exit(1)
		}
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

	bus := events.New()
	libraryRepo, err := db.NewLibraryRepo(database, cfg.Library.Paths)
	if err != nil {
		slog.Error("failed to create library repo", "error", err)
		_ = database.Close()
		os.Exit(1)
	}

	searchIndex := app.NewSearchIndex(libraryRepo)
	peerRepo := db.NewPeerRepo(database)

	scanner := app.NewLibraryScanner(libraryRepo, searchIndex, bus, cfg.Library.Paths)
	searchService := app.NewSearchService(searchIndex, libraryRepo)

	resolver := app.NewResolver(libraryRepo)
	player := audio.NewEngine(cfg.Playback.SampleRate)
	queue := audio.NewQueue()

	playback := app.NewPlaybackController(libraryRepo, searchIndex, player, resolver, bus)
	ipcServer := ipc.NewServer(cfg.Daemon.SocketPath)
	ipcServer.SetPlaybackHandler(playback)
	ipcServer.SetScannerHandler(scanner)
	ipcServer.SetSearchHandler(searchService)
	ipcServer.SetLibraryRepoHandler(libraryRepo)
	ipcServer.SetPeerRepoHandler(peerRepo)
	ipcServer.SetQueueHandler(queue)
	if err := ipcServer.Start(context.Background()); err != nil {
		slog.Error("failed to start IPC server", "error", err)
		_ = database.Close()
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

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-sigCtx.Done()

	slog.Info("shutting down", "reason", context.Cause(sigCtx))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_ = ipcServer.Stop(shutdownCtx)
	if err := player.Stop(shutdownCtx); err != nil {
		slog.Warn("player stop error", "error", err)
	}

	_ = database.Close()
	slog.Info("raag daemon stopped")
}

func usage() {
	fmt.Fprintf(os.Stderr, `Raag - Terminal music player with P2P streaming

Usage: raagd [flags]

Flags:
  --music-path path   Music directory path (overrides config)
  --socket path       IPC socket path (default ~/.local/share/raag/raag.sock)
  --data-dir path     Data directory (default ~/.local/share/raag)
  --no-scan           Skip library scan on startup
  -h, --help          Show this help

On First Run:
  Simply run 'raagd' and you'll be guided through setup automatically.

Examples:
  raagd                            # First run: automatic setup wizard
  raagd                            # Subsequent runs: start daemon
  raagd --music-path ~/Music      # Override music path

`)
}

func isPathNotConfiguredError(err error) bool {
	return errors.Is(err, config.ErrNoMusicPath)
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
