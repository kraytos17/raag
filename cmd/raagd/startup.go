package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/p-society/raag/internal/app"
	"github.com/p-society/raag/internal/config"
	"github.com/p-society/raag/internal/infra/duplicate"
	"github.com/p-society/raag/internal/infra/observability"
	db "github.com/p-society/raag/internal/infra/storage"
)

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
