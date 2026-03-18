package app

import (
	"context"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/p-society/raag/internal/domain"
)

type LibraryScanner struct {
	libraryRepo LibraryRepository
	index       SearchIndex
	bus         domain.EventBus
	paths       []string
}

func NewLibraryScanner(
	libraryRepo LibraryRepository,
	index SearchIndex,
	bus domain.EventBus,
	paths []string,
) *LibraryScanner {
	return &LibraryScanner{
		libraryRepo: libraryRepo,
		index:       index,
		bus:         bus,
		paths:       paths,
	}
}

func (s *LibraryScanner) Scan(ctx context.Context) (int, error) {
	startTime := time.Now()
	s.bus.Publish(ctx, domain.NewEvent(domain.EventScanStarted, domain.ScanStartedPayload{
		Paths:     s.paths,
		StartTime: startTime,
	}))

	var totalScanned int
	var totalAdded int
	for _, scanPath := range s.paths {
		scanned, added, err := s.scanDirectory(ctx, scanPath)
		if err != nil {
			slog.Error("scan directory failed", "path", scanPath, "error", err)
			continue
		}

		totalScanned += scanned
		totalAdded += added
	}

	duration := time.Since(startTime)
	s.bus.Publish(ctx, domain.NewEvent(domain.EventScanComplete, domain.ScanCompletePayload{
		Scanned:  totalScanned,
		Added:    totalAdded,
		Removed:  0,
		Duration: duration,
		Errors:   []string{},
	}))
	return totalScanned, nil
}

func (s *LibraryScanner) ScanIncremental(ctx context.Context) (added int, modified int, removed int, err error) {
	currentStats := make(map[string]*domain.FileStat)
	previousStats := make(map[string]*domain.FileStat)
	for _, scanPath := range s.paths {
		files := s.walkDirectory(scanPath)
		for _, file := range files {
			stat, err := s.getFileStat(file)
			if err != nil {
				continue
			}
			currentStats[file] = stat
		}
	}

	prevStats, err := s.libraryRepo.LoadFileStats(ctx)
	if err != nil {
		return 0, 0, 0, err
	}

	maps.Copy(previousStats, prevStats)
	for path, current := range currentStats {
		previous, exists := previousStats[path]
		if !exists {
			added++
		} else if current.Changed(previous) {
			modified++
		}
	}
	for path := range previousStats {
		if _, exists := currentStats[path]; !exists {
			removed++
		}
	}
	if added > 0 || modified > 0 {
		if _, err := s.Scan(ctx); err != nil {
			return 0, 0, 0, err
		}
	}
	return added, modified, removed, nil
}

func (s *LibraryScanner) scanDirectory(ctx context.Context, dirPath string) (int, int, error) {
	var scanned int
	var added int

	files := s.walkDirectory(dirPath)
	for i, file := range files {
		select {
		case <-ctx.Done():
			return scanned, added, ctx.Err()
		default:
		}

		s.bus.Publish(ctx, domain.NewEvent(domain.EventScanComplete, ScanProgressPayload{
			Scanned:     i + 1,
			Total:       len(files),
			CurrentFile: file,
			Phase:       ScanPhaseParsing,
		}))

		track, err := s.parseFile(file)
		if err != nil {
			slog.Warn("failed to parse file", "file", file, "error", err)
			continue
		}
		if err := s.libraryRepo.Save(ctx, track); err != nil {
			slog.Warn("failed to save track", "track", track.ID, "error", err)
			continue
		}
		if err := s.index.Index(ctx, track); err != nil {
			slog.Warn("failed to index track", "track", track.ID, "error", err)
		}

		scanned++
		added++
	}
	return scanned, added, nil
}

func (s *LibraryScanner) walkDirectory(dirPath string) []string {
	var files []string
	filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".mp3" || ext == ".flac" || ext == ".ogg" || ext == ".wav" || ext == ".m4a" {
			files = append(files, path)
		}
		return nil
	})
	return files
}

func (s *LibraryScanner) parseFile(path string) (*domain.Track, error) {
	return nil, nil
}

func (s *LibraryScanner) getFileStat(path string) (*domain.FileStat, error) {
	return nil, nil
}
