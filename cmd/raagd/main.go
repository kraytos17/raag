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
	database, libraryRepo, p2pNode, p2pEnabled, searchService, metrics := initializeServices(cfg)
	runDaemon(cfg, database, libraryRepo, p2pNode, p2pEnabled, searchService, skipScan, metrics)
}

func initializeServices(cfg *config.Config) (*db.DB, db.LibraryRepo, *p2p.P2PNode, bool, *app.SearchService, *observability.Metrics) {
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

	metrics := observability.NewMetrics()

	searchIndex := app.NewSearchIndex(libraryRepo)
	searchService := app.NewSearchService(searchIndex, libraryRepo)
	enabled := cfg.P2P.Enabled
	if !enabled {
		return database, libraryRepo, nil, false, searchService, metrics
	}

	peerRepo := db.NewPeerRepo(database)
	p2pNode, err := p2p.NewP2PNode(p2p.P2PNodeConfig{
		DataDir:            cfg.Daemon.DataDir,
		ListenAddrs:        cfg.P2P.ListenAddrs,
		AnnounceAddrs:      cfg.P2P.AnnounceAddrs,
		BootstrapPeers:     cfg.P2P.BootstrapPeers,
		MdnsServiceName:    cfg.P2P.MDNSServiceTag,
		ShareManifest:      cfg.Privacy.ShareLibraryManifest,
		ConnMgrLowMark:     cfg.P2P.ConnMgrLowMark,
		ConnMgrHighMark:    cfg.P2P.ConnMgrHighMark,
		ConnMgrGrace:       cfg.P2P.ConnMgrGrace,
		LANOnly:            cfg.P2P.LANOnly,
		MaxPeers:           cfg.P2P.MaxPeers,
		StreamPort:         cfg.P2P.StreamPort,
		PerPeerRateLimit:   cfg.P2P.PerPeerRateLimit,
		UploadBandwidth:    int64(cfg.P2P.UploadBandwidth),
		CBFailureThreshold: cfg.P2P.CBFailureThreshold,
		CBCooldown:         cfg.P2P.CBCooldown,
		MaxKnownPeers:      cfg.P2P.MaxKnownPeers,
		ChunkSize:          cfg.P2P.ChunkSize,
		PeerDataTTL:        cfg.P2P.PeerDataTTL,
		Transcoder:         transcoder.New(transcoder.Config{FFmpegPath: cfg.Transcoder.FFmpegPath, StreamCodec: cfg.Transcoder.StreamCodec, StreamBitrate: cfg.Transcoder.StreamBitrate}),
		PeerRepo:           peerRepo,
		Search:             searchService,
		Metrics:            metrics,
	}, libraryRepo)
	if err != nil {
		slog.Error("failed to create P2P node", "error", err)
		return database, libraryRepo, nil, false, searchService, metrics
	}
	return database, libraryRepo, p2pNode, true, searchService, metrics
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

func runDaemon(cfg *config.Config, database *db.DB, libraryRepo db.LibraryRepo,
	p2pNode *p2p.P2PNode, p2pEnabled bool, searchService *app.SearchService,
	skipScan bool, metrics *observability.Metrics,
) {
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
		resolver = app.NewMultiSourceResolver(libraryRepo, p2pResolver)
		searchService.SetRemote(resolver.(app.RemoteSearcher))
		p2pNodeStarted = true
	} else {
		resolver = app.NewLocalResolver(libraryRepo)
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

// startIndexAndScan rebuilds the search index when the scan is skipped (so
// search works without one), otherwise kicks off the library scan.
func startIndexAndScan(sigCtx context.Context, cfg *config.Config, skipScan bool,
	searchService *app.SearchService, libraryRepo db.LibraryRepo,
	scanner *app.LibraryScanner, duplicateDetector *duplicate.Detector,
) {
	if skipScan {
		if err := searchService.Index().Rebuild(sigCtx, libraryRepo); err != nil {
			slog.Warn("failed to rebuild search index at startup", "error", err)
		} else {
			slog.Info("search index rebuilt from database")
		}
		return
	}
	if !cfg.Library.ScanOnStart {
		return
	}

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

// startMetricsServer exposes the Prometheus metrics endpoint on the given
// instance. Returns the instance so callers can keep wiring collectors.
func startMetricsServer(ctx context.Context, cfg config.MetricsConfig, metrics *observability.Metrics) *observability.Metrics {
	if metrics == nil {
		metrics = observability.NewMetrics()
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	if err := metrics.Start(ctx, addr, cfg.Path); err != nil {
		slog.Error("failed to start metrics server", "error", err)
	}
	return metrics
}

// applyConfiguredVolume pushes the configured volume into the engine so it is
// applied on the first playback (the controller's field alone does not touch
// the engine).
func applyConfiguredVolume(playback *app.PlaybackController, volume int) {
	if err := playback.SetVolume(context.Background(), volume); err != nil {
		slog.Warn("failed to apply configured volume", "error", err)
	}
}

// warnUnsupportedOutputDevice acknowledges a non-default output_device config.
// The beep/oto backend cannot select an output device, so only the default
// ALSA/OS device is supported.
func warnUnsupportedOutputDevice(device string) {
	if device != "" && device != "default" {
		slog.Warn("playback.output_device is not supported by the current audio backend; using the default device", "device", device)
	}
}

// dspFromConfig maps the playback config to the engine's DSP processing.
func dspFromConfig(cfg config.PlaybackConfig) audio.DSPConfig {
	return audio.DSPConfig{
		Equalizer: audio.EqualizerConfig{
			Enabled: cfg.Equalizer.Enabled,
			Bass:    cfg.Equalizer.Bass,
			Mid:     cfg.Equalizer.Mid,
			Treble:  cfg.Equalizer.Treble,
		},
		Normalize: audio.NormalizeConfig{
			Enabled:  cfg.Normalize.Enabled,
			TargetDB: cfg.Normalize.TargetDB,
		},
		Crossfade: time.Duration(cfg.CrossfadeMs) * time.Millisecond,
	}
}

// loadPersistedVolume returns the restart-surviving volume from the DB, falling
// back to the config default when nothing is persisted.
func loadPersistedVolume(cfg *config.Config, settings app.SettingsRepository) int {
	volume := cfg.Playback.Volume
	if persisted, ok, err := settings.GetVolume(context.Background()); err == nil && ok {
		volume = persisted
	}
	return volume
}

// persistVolumeChanges writes every volume change to the DB so the setting
// survives restarts. Failures are logged, never fatal.
func persistVolumeChanges(settings app.SettingsRepository, bus *events.EventBus) {
	bus.Subscribe(domain.EventVolumeChanged, func(e domain.Event) {
		payload, ok := e.Payload.(domain.VolumeChangedPayload)
		if !ok {
			return
		}
		if err := settings.SetVolume(context.Background(), payload.Volume); err != nil {
			slog.Warn("failed to persist volume", "error", err)
		}
	})
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
