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
	"golang.org/x/term"
)

func main() {
	cfg, skipScan := loadConfig()
	database, libraryRepo, p2pNode := initializeServices(cfg)
	runDaemon(cfg, database, libraryRepo, p2pNode, skipScan)
}

func initializeServices(cfg *config.Config) (*db.DB, db.LibraryRepo, *p2p.P2PNode) {
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

	p2pNode, err := p2p.NewP2PNode(p2p.P2PNodeConfig{
		DataDir:         cfg.Daemon.DataDir,
		ListenAddrs:     cfg.P2P.ListenAddrs,
		AnnounceAddrs:   nil,
		BootstrapPeers:  cfg.P2P.BootstrapPeers,
		MdnsServiceName: cfg.P2P.MDNSServiceTag,
		ShareManifest:   cfg.Privacy.ShareLibraryManifest,
		ConnMgrLowMark:  cfg.P2P.ConnMgrLowMark,
		ConnMgrHighMark: cfg.P2P.ConnMgrHighMark,
		ConnMgrGrace:    cfg.P2P.ConnMgrGrace,
	}, libraryRepo)
	if err != nil {
		slog.Error("failed to create P2P node", "error", err)
		return database, libraryRepo, nil
	}
	return database, libraryRepo, p2pNode
}

func loadConfig() (*config.Config, bool) {
	setup := flag.Bool("setup", false, "Run first-time setup wizard")
	musicPath := flag.String("music-path", "", "Music directory path")
	socketPath := flag.String("socket", "", "IPC socket path (overrides config)")
	dataDir := flag.String("data-dir", "", "Data directory (overrides config)")
	noScan := flag.Bool("no-scan", false, "Skip library scan on startup")
	p2pEnabled := flag.Bool("p2p", false, "Enable P2P networking (overrides config)")
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
	if flag.Lookup("p2p") != nil && flag.Parsed() {
		if *p2pEnabled {
			cfg.P2P.Enabled = true
		}
	}

	slog.SetDefault(observability.NewLogger(observability.Config{
		Level:     observability.ParseLevel(cfg.Daemon.LogLevel),
		AddSource: true,
	}))
	return cfg, *noScan
}

func runDaemon(cfg *config.Config, database *db.DB, libraryRepo db.LibraryRepo, p2pNode *p2p.P2PNode, skipScan bool) {
	bus := events.New()
	searchIndex := app.NewSearchIndex(libraryRepo)
	peerRepo := db.NewPeerRepo(database)
	duplicateDetector := duplicate.NewDetector(database, duplicate.ConfigToHandler(cfg.Library.DuplicateHandling))
	if p2pNode != nil {
		if err := p2pNode.Start(context.Background(), bus); err != nil {
			slog.Warn("failed to start P2P node", "error", err)
		}
	}

	scanner := app.NewLibraryScanner(libraryRepo, libraryRepo, searchIndex, bus, cfg.Library.Paths)
	scanner.SetDuplicateCheck(func(ctx context.Context, hash string, trackID domain.TrackID, path string) (domain.TrackID, bool, bool, error) {
		return duplicateDetector.CheckDuplicate(ctx, hash, trackID, path)
	})

	searchService := app.NewSearchService(searchIndex, libraryRepo)

	var resolver app.Resolver
	if cfg.P2P.Enabled && p2pNode != nil {
		p2pResolver := p2p.NewP2PResolverAdapter(p2pNode.Resolver())
		resolver = app.NewMultiSourceResolver(libraryRepo, p2pResolver)
		slog.Info("P2P streaming enabled", "peer_id", p2pNode.ID())
	} else {
		resolver = app.NewLocalResolver(libraryRepo)
	}

	player := audio.NewEngine(cfg.Playback.SampleRate)
	queue := audio.NewQueue()
	playback := app.NewPlaybackController(libraryRepo, searchService, player, resolver, bus)
	playback.SetQueue(queue)
	cbThreshold := cfg.P2P.CBFailureThreshold
	if cbThreshold <= 0 {
		cbThreshold = 5
	}

	cbCooldown := cfg.P2P.CBCooldown
	if cbCooldown <= 0 {
		cbCooldown = time.Minute
	}

	cbRegistry := app.NewCBRegistry(cbThreshold, cbCooldown)
	ipcServer, err := ipc.NewServer(cfg.Daemon.SocketPath, ipc.ServerConfig{
		Playback:    playback,
		Scanner:     scanner,
		Search:      searchService,
		LibraryRepo: libraryRepo,
		PeerRepo:    peerRepo,
		Queue:       queue,
		CBRegistry:  cbRegistry,
		P2PNode:     p2pNode,
	})
	if err != nil {
		slog.Error("failed to create IPC server", "error", err)
		_ = database.Close()
		os.Exit(1)
	}

	lc := app.NewLifecycleManager()
	if err := lc.Register(scanner); err != nil {
		slog.Error("failed to register component", "error", err)
		_ = database.Close()
		os.Exit(1)
	}
	if err := lc.Register(ipc.AsComponent(ipcServer)); err != nil {
		slog.Error("failed to register component", "error", err)
		_ = database.Close()
		os.Exit(1)
	}
	if err := lc.StartAll(context.Background()); err != nil {
		slog.Error("failed to start components", "error", err)
		_ = database.Close()
		os.Exit(1)
	}
	if cfg.Library.ScanOnStart && !skipScan {
		slog.Info("starting library scan", "paths", cfg.Library.Paths)
		go func() {
			if _, err := scanner.Scan(context.Background()); err != nil {
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

	if err := player.Stop(shutdownCtx); err != nil {
		slog.Warn("player stop error", "error", err)
	}
	if p2pNode != nil {
		if err := p2pNode.Stop(shutdownCtx); err != nil {
			slog.Warn("P2P node stop error", "error", err)
		}
	}

	_ = lc.StopAll(shutdownCtx)
	bus.Close()
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
