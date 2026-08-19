package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/domain"
	"github.com/p-society/raag/internal/infra/audio"
	"github.com/p-society/raag/internal/infra/duplicate"
	"github.com/p-society/raag/internal/infra/events"
	"github.com/p-society/raag/internal/infra/ipc"
	"github.com/p-society/raag/internal/infra/observability"
	"github.com/p-society/raag/internal/infra/p2p"
	db "github.com/p-society/raag/internal/infra/storage"
	"github.com/p-society/raag/internal/infra/transcoder"
)

func runDaemon(cfg *config.Config, database *db.DB, libraryRepo db.LibraryRepo,
	p2pNode *p2p.P2PNode, p2pEnabled bool, searchService *app.SearchService,
	skipScan bool, metrics *observability.Metrics, tr *transcoder.Transcoder,
) {
	if err := checkStalePidFile(cfg.Daemon.PidFile); err != nil {
		if errors.Is(err, ErrAlreadyRunning) {
			slog.Error("refusing to start: another raagd daemon is already running",
				"pid_file", cfg.Daemon.PidFile)
		} else {
			slog.Error("failed to check pid file", "pid_file", cfg.Daemon.PidFile, "error", err)
		}
		_ = database.Close()
		os.Exit(1)
	}

	bus := events.New()
	peerRepo := db.NewPeerRepo(database)
	settings := db.NewSettingsRepo(database)
	duplicateDetector := duplicate.NewDetector(database, duplicate.ConfigToHandler(cfg.Library.DuplicateHandling))
	database.StartHealthCheck(10*time.Second, func() {
		slog.Error("database directory deleted, initiating shutdown")
		bus.Close()
		if cfg.Daemon.SocketPath != "" {
			os.Remove(cfg.Daemon.SocketPath)
		}

		removePidFile(cfg.Daemon.PidFile)
		_ = database.Close()
		os.Exit(1)
	})

	scanner := app.NewLibraryScanner(libraryRepo, libraryRepo, searchService.Index(), bus, cfg.Library.Paths)
	scanner.SetDuplicateCheck(func(ctx context.Context, hash string, trackID domain.TrackID, path string) (domain.TrackID, bool, bool, error) {
		return duplicateDetector.CheckDuplicate(ctx, hash, trackID, path)
	})
	scanner.EnableHashing(2)

	var resolver app.Resolver
	var p2pNodeStarted bool
	if p2pEnabled && p2pNode != nil {
		p2pResolver := p2p.NewP2PResolverAdapter(p2pNode.Resolver())
		p2pResolver.SetPeersProvider(p2pNode.Peers)
		resolver = app.NewMultiSourceResolver(libraryRepo, p2pResolver, tr)
		searchService.SetRemote(resolver.(app.RemoteSearcher))
		p2pNodeStarted = true
	} else {
		resolver = app.NewLocalResolver(libraryRepo, tr)
	}

	player := audio.NewEngine(cfg.Playback.SampleRate, cfg.Playback.BufferSize, dspFromConfig(cfg.Playback))
	warnUnsupportedOutputDevice(cfg.Playback.OutputDevice)
	queue := audio.NewQueue()

	// A persisted volume (from the DB) overrides the config default, which is
	// only a fallback. The config file itself is never rewritten.
	initialVolume := loadPersistedVolume(cfg, settings)
	playback := app.NewPlaybackController(libraryRepo, searchService, player, resolver, bus, initialVolume)

	playback.SetQueue(queue)
	applyConfiguredVolume(playback, initialVolume)
	persistVolumeChanges(settings, bus)
	ipcServer, err := ipc.NewServer(cfg.Daemon.SocketPath, ipc.ServerConfig{
		Playback:     playback,
		Scanner:      scanner,
		Search:       searchService,
		LibraryRepo:  libraryRepo,
		PeerRepo:     peerRepo,
		Queue:        queue,
		PlaylistRepo: db.NewPlaylistRepo(database),
		P2PNode:      p2pNode,
		EventBus:     bus,
		Metrics:      metrics,
		DBStats:      database,
	})
	if err != nil {
		slog.Error("failed to create IPC server", "error", err)
		_ = database.Close()
		os.Exit(1)
	}

	lc := app.NewLifecycleManager()
	if err := lc.Register(scanner); err != nil {
		slog.Error("failed to register scanner", "error", err)
		_ = database.Close()
		os.Exit(1)
	}
	if err := lc.Register(app.NewPlayerComponent(player, "player")); err != nil {
		slog.Error("failed to register player", "error", err)
		_ = database.Close()
		os.Exit(1)
	}
	if p2pNodeStarted && p2pNode != nil {
		if err := lc.Register(app.NewP2PComponent(p2pNode, bus, "p2p")); err != nil {
			slog.Error("failed to register p2p", "error", err)
			_ = database.Close()
			os.Exit(1)
		}
	}
	if err := lc.Register(ipc.AsComponent(ipcServer)); err != nil {
		slog.Error("failed to register ipc server", "error", err)
		_ = database.Close()
		os.Exit(1)
	}
	if cfg.Library.Watch {
		registerFileWatcher(lc, cfg.Library.Paths, scanner, database)
	}
	if err := lc.StartAll(context.Background()); err != nil {
		slog.Error("failed to start components", "error", err)
		_ = database.Close()
		os.Exit(1)
	}
	if err := writePidFile(cfg.Daemon.PidFile); err != nil {
		slog.Warn("failed to write pid file", "path", cfg.Daemon.PidFile, "error", err)
	}
	if p2pNodeStarted && p2pNode != nil {
		slog.Info("P2P streaming enabled", "peer_id", p2pNode.ID())
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	if cfg.Metrics.Enabled {
		startMetricsServer(sigCtx, cfg.Metrics, metrics)
	}

	startIndexAndScan(sigCtx, cfg, skipScan, searchService, libraryRepo, scanner, duplicateDetector)
	slog.Info(
		"raag daemon started",
		"socket", cfg.Daemon.SocketPath,
		"data_dir", cfg.Daemon.DataDir,
		"volume", cfg.Playback.Volume,
	)

	if cfg.P2P.Enabled {
		slog.Info(
			"P2P networking enabled",
			"ports", "7844/TCP+UDP, 7845/UDP",
			"note", "ensure firewall allows inbound connections on these ports for LAN discovery",
		)
	}

	<-sigCtx.Done()
	slog.Info("shutting down", "reason", context.Cause(sigCtx))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	defer stop()

	if err := lc.StopAll(shutdownCtx); err != nil {
		slog.Warn("lifecycle stop error", "error", err)
	}

	removePidFile(cfg.Daemon.PidFile)
	bus.Close()
	if tr != nil {
		tr.Cleanup()
	}

	slog.Info("closing database...")
	if err := database.CloseWithContext(shutdownCtx); err != nil {
		slog.Error("database close failed", "error", err)
	} else {
		slog.Info("database closed")
	}
	slog.Info("raag daemon stopped")
}
