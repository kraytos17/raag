package main

import (
	"log/slog"
	"os"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/infra/observability"
	"github.com/p-society/raag/internal/infra/p2p"
	db "github.com/p-society/raag/internal/infra/storage"
	"github.com/p-society/raag/internal/infra/transcoder"
)

func initializeServices(cfg *config.Config, tr *transcoder.Transcoder) (*db.DB, db.LibraryRepo, *p2p.P2PNode, bool, *app.SearchService, *observability.Metrics) {
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
		BroadcastEnabled:   cfg.P2P.BroadcastEnabled,
		BroadcastPort:      cfg.P2P.BroadcastPort,
		LibraryPaths:       cfg.Library.Paths,
		Transcoder:         tr,
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
