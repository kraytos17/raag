package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/p-society/raag/internal/domain"
)

type LibraryManager struct {
	scanner *LibraryScanner
}

func NewLibraryManager(scanner *LibraryScanner) *LibraryManager {
	return &LibraryManager{
		scanner: scanner,
	}
}

func (m *LibraryManager) ScanLibrary(ctx context.Context) (int, error) {
	return m.scanner.Scan(ctx)
}

func (m *LibraryManager) ScanIncremental(ctx context.Context) (added int, modified int, removed int, err error) {
	return m.scanner.ScanIncremental(ctx)
}

type ScanProgress struct {
	Phase       string
	TotalFound  int
	Processed   int
	CurrentFile string
	Errors      []error
}

type Scanner struct {
	paths         []string
	supportedExts map[string]bool
	onProgress    func(ScanProgress)
	onTrack       func(*domain.Track)
	onError       func(error)
}

func NewScanner(paths []string) *Scanner {
	exts := make(map[string]bool)
	for _, ext := range []string{".mp3", ".flac", ".ogg", ".wav", ".m4a", ".aac", ".opus", ".wma"} {
		exts[ext] = true
	}
	return &Scanner{
		paths:         paths,
		supportedExts: exts,
	}
}

func (s *Scanner) OnProgress(fn func(ScanProgress)) {
	s.onProgress = fn
}

func (s *Scanner) OnTrack(fn func(*domain.Track)) {
	s.onTrack = fn
}

func (s *Scanner) OnError(fn func(error)) {
	s.onError = fn
}

func (s *Scanner) Scan(ctx context.Context) ([]string, error) {
	var files []string

	for _, path := range s.paths {
		if err := filepath.Walk(path, func(walkPath string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			if info.IsDir() {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(walkPath))
			if s.supportedExts[ext] {
				files = append(files, walkPath)
				if s.onProgress != nil {
					s.onProgress(ScanProgress{
						Phase:      "discovery",
						TotalFound: len(files),
					})
				}
			}
			return nil
		}); err != nil {
			if s.onError != nil {
				s.onError(err)
			}
		}
	}

	return files, nil
}
