package main

import (
	"context"
	"flag"
	"fmt"
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
	"golang.org/x/term"
)

func main() {
	cfg, skipScan := loadConfig()
	database, libraryRepo, p2pNode, p2pEnabled := initializeServices(cfg)
	runDaemon(cfg, database, libraryRepo, p2pNode, p2pEnabled, skipScan)
}

func initializeServices(cfg *config.Config) (*db.DB, db.LibraryRepo, *p2p.P2PNode, bool) {
	dbOpts := db.DefaultOptions(cfg.Daemon.DataDir)
	database, err := db.Open(cfg.Daemon.DataDir, dbOpts)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}

	libraryRepo, err := db.NewLibraryRepo(database, cfg.Library.Paths)
	if err != nil {
		slog.Error("failed to create library repo", "error", err)
		database.Close()
		os.Exit(1)
	}

	enabled := cfg.P2P.Enabled
	if !enabled {
		return database, libraryRepo, nil, false
	}

	peerRepo := db.NewPeerRepo(database)
	p2pNode, err := p2p.NewP2PNode(p2p.P2PNodeConfig{
		DataDir:         cfg.Daemon.DataDir,
		ListenAddrs:     cfg.P2P.ListenAddrs,
		AnnounceAddrs:   cfg.P2P.AnnounceAddrs,
		BootstrapPeers:  cfg.P2P.BootstrapPeers,
		MdnsServiceName: cfg.P2P.MDNSServiceTag,
		ShareManifest:   cfg.Privacy.ShareLibraryManifest,
		ConnMgrLowMark:  cfg.P2P.ConnMgrLowMark,
		ConnMgrHighMark: cfg.P2P.ConnMgrHighMark,
		ConnMgrGrace:    cfg.P2P.ConnMgrGrace,
		LANOnly:         cfg.P2P.LANOnly,
		MaxKnownPeers:   cfg.P2P.MaxKnownPeers,
		ChunkSize:       cfg.P2P.ChunkSize,
		PeerDataTTL:     cfg.P2P.PeerDataTTL,
		Transcoder:      transcoder.New(transcoder.Config{FFmpegPath: cfg.Transcoder.FFmpegPath, StreamCodec: cfg.Transcoder.StreamCodec, StreamBitrate: cfg.Transcoder.StreamBitrate}),
		PeerRepo:        peerRepo,
	}, libraryRepo)
	if err != nil {
		slog.Error("failed to create P2P node", "error", err)
		return database, libraryRepo, nil, false
	}
	return database, libraryRepo, p2pNode, true
}

func loadConfig() (*config.Config, bool) {
	setup := flag.Bool("setup", false, "Run first-time setup wizard")
	musicPath := flag.String("music-path", "", "Music directory path")
	socketPath := flag.String("socket", "", "IPC socket path (overrides config)")
	dataDir := flag.String("data-dir", "", "Data directory (overrides config)")
	noScan := flag.Bool("no-scan", false, "Skip library scan on startup")
	p2pFlag := flag.Bool("p2p", false, "Enable P2P networking (use --p2p=false to disable)")
	flag.Usage = usage
	flag.Parse()

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

func runDaemon(cfg *config.Config, database *db.DB, libraryRepo db.LibraryRepo, p2pNode *p2p.P2PNode, p2pEnabled bool, skipScan bool) {
	bus := events.New()
	searchIndex := app.NewSearchIndex(libraryRepo)
	peerRepo := db.NewPeerRepo(database)
	duplicateDetector := duplicate.NewDetector(database, duplicate.ConfigToHandler(cfg.Library.DuplicateHandling))
	database.StartHealthCheck(10*time.Second, func() {
		slog.Error("database directory deleted, initiating shutdown")
		bus.Close()
		if cfg.Daemon.SocketPath != "" {
			os.Remove(cfg.Daemon.SocketPath)
		}
		os.Exit(1)
	})

	scanner := app.NewLibraryScanner(libraryRepo, libraryRepo, searchIndex, bus, cfg.Library.Paths)
	scanner.SetDuplicateCheck(func(ctx context.Context, hash string, trackID domain.TrackID, path string) (domain.TrackID, bool, bool, error) {
		return duplicateDetector.CheckDuplicate(ctx, hash, trackID, path)
	})

	scanner.EnableHashing(2)
	searchService := app.NewSearchService(searchIndex, libraryRepo)

	var resolver app.Resolver
	var p2pNodeStarted bool
	if p2pEnabled && p2pNode != nil {
		p2pResolver := p2p.NewP2PResolverAdapter(p2pNode.Resolver())
		resolver = app.NewMultiSourceResolver(libraryRepo, p2pResolver)
		p2pNodeStarted = true
	} else {
		resolver = app.NewLocalResolver(libraryRepo)
	}

	player := audio.NewEngine(cfg.Playback.SampleRate)
	queue := audio.NewQueue()
	playback := app.NewPlaybackController(libraryRepo, searchService, player, resolver, bus, cfg.Playback.Volume)
	playback.SetQueue(queue)
	applyConfiguredVolume(playback, cfg.Playback.Volume)
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
	if p2pNodeStarted && p2pNode != nil {
		slog.Info("P2P streaming enabled", "peer_id", p2pNode.ID())
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	if cfg.Metrics.Enabled {
		startMetricsServer(sigCtx, cfg.Metrics)
	}
	if cfg.Library.ScanOnStart && !skipScan {
		slog.Info("starting library scan", "paths", cfg.Library.Paths)
		go func() {
			if _, err := scanner.Scan(sigCtx); err != nil {
				if sigCtx.Err() != nil {
					slog.Info("library scan canceled due to shutdown")
					return
				}
				slog.Error("library scan failed", "error", err)
			}
			if dups := duplicateDetector.GetDuplicates(); len(dups) > 0 {
				slog.Warn("duplicates detected", "groups", len(dups))
				for _, dup := range dups {
					slog.Info("duplicate group", "hash", dup.Hash[:16]+"...", "original", dup.OriginalID, "duplicates", len(dup.DuplicateIDs))
				}
			}
		}()
	}

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

	bus.Close()
	slog.Info("closing database...")
	if err := database.CloseWithContext(shutdownCtx); err != nil {
		slog.Error("database close failed", "error", err)
	} else {
		slog.Info("database closed")
	}
	slog.Info("raag daemon stopped")
}

// registerFileWatcher wires the library file watcher into the lifecycle
// manager. On failure it closes the DB and exits.
func registerFileWatcher(lc *app.LifecycleManager, paths []string, scanner *app.LibraryScanner, database *db.DB) {
	fw, err := app.NewFileWatcher(paths, scanner)
	if err != nil {
		slog.Error("failed to create file watcher", "error", err)
		_ = database.Close()
		os.Exit(1)
	}
	if err := lc.Register(app.NewFileWatcherComponent(fw, "filewatcher")); err != nil {
		slog.Error("failed to register file watcher", "error", err)
		_ = database.Close()
		os.Exit(1)
	}
}

// startMetricsServer exposes the Prometheus metrics endpoint.
func startMetricsServer(ctx context.Context, cfg config.MetricsConfig) {
	metrics := observability.NewMetrics()
	addr := fmt.Sprintf(":%d", cfg.Port)
	if err := metrics.Start(ctx, addr, cfg.Path); err != nil {
		slog.Error("failed to start metrics server", "error", err)
	}
}

// applyConfiguredVolume pushes the configured volume into the engine so it is
// applied on the first playback (the controller's field alone does not touch
// the engine).
func applyConfiguredVolume(playback *app.PlaybackController, volume int) {
	if err := playback.SetVolume(context.Background(), volume); err != nil {
		slog.Warn("failed to apply configured volume", "error", err)
	}
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
