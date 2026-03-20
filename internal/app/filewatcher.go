package app

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/p-society/raag/internal/domain"
)

type FileWatcher struct {
	watcher   *fsnotify.Watcher
	paths     []string
	scanner   *LibraryScanner
	statCache map[string]*domain.FileStat
	mu        sync.Mutex
	pending   map[string]fsnotify.Event
	flushTick *time.Ticker
}

func NewFileWatcher(paths []string, scanner *LibraryScanner) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fw := &FileWatcher{
		watcher:   watcher,
		paths:     paths,
		scanner:   scanner,
		statCache: make(map[string]*domain.FileStat),
	}
	for _, path := range paths {
		if err := filepath.WalkDir(path, func(walkPath string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return fw.watcher.Add(walkPath)
			}
			return nil
		}); err != nil {
			watcher.Close()
			return nil, err
		}
	}
	return fw, nil
}

func (fw *FileWatcher) Start(ctx context.Context) {
	go fw.run(ctx)
}

func (fw *FileWatcher) run(ctx context.Context) {
	fw.flushTick = time.NewTicker(100 * time.Millisecond)
	defer fw.flushTick.Stop()

	for {
		select {
		case <-ctx.Done():
			fw.watcher.Close()
			fw.flushPending(ctx)
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			fw.queueEvent(event)
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("fsnotify error", "error", err)
		case <-fw.flushTick.C:
			fw.flushPending(ctx)
		}
	}
}

func (fw *FileWatcher) queueEvent(event fsnotify.Event) {
	path := event.Name
	if !fw.isAudioFile(path) {
		return
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.pending == nil {
		fw.pending = make(map[string]fsnotify.Event)
	}
	if event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename) {
		delete(fw.pending, path)
		delete(fw.statCache, path)
		fw.pending[path] = event
		return
	}
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
		fw.pending[path] = event
	}
}

func (fw *FileWatcher) flushPending(ctx context.Context) {
	fw.mu.Lock()
	pending := fw.pending
	fw.pending = make(map[string]fsnotify.Event)
	fw.mu.Unlock()

	for path := range pending {
		if info, err := os.Stat(path); os.IsNotExist(err) {
			fw.removeFile(ctx, path)
		} else if err == nil && !info.IsDir() {
			fw.mu.Lock()
			prev, exists := fw.statCache[path]
			newStat := &domain.FileStat{
				Path:  path,
				Mtime: info.ModTime().Unix(),
				Size:  info.Size(),
			}
			if !exists || prev.Mtime != newStat.Mtime || prev.Size != newStat.Size {
				fw.statCache[path] = newStat
				fw.mu.Unlock()
				fw.processFile(ctx, path)
			} else {
				fw.statCache[path] = newStat
				fw.mu.Unlock()
			}
		}
	}
}

func (fw *FileWatcher) removeFile(ctx context.Context, path string) {
	track, err := fw.scanner.LibraryRepo().FindByPath(ctx, path)
	if err != nil {
		return
	}
	if err := fw.scanner.LibraryRepo().Delete(ctx, track.ID); err != nil {
		slog.Warn("failed to delete removed track", "path", path, "error", err)
	}
	if err := fw.scanner.Index().Delete(ctx, track.ID); err != nil {
		slog.Warn("failed to delete from index", "path", path, "error", err)
	}
}

func (fw *FileWatcher) processFile(ctx context.Context, path string) {
	track, err := fw.scanner.parseFile(path)
	if err != nil {
		slog.Warn("failed to parse changed file", "path", path, "error", err)
		return
	}
	if err := fw.scanner.LibraryRepo().Save(ctx, track); err != nil {
		slog.Warn("failed to save changed track", "path", path, "error", err)
		return
	}
	if err := fw.scanner.Index().Index(ctx, track); err != nil {
		slog.Warn("failed to index changed track", "path", path, "error", err)
	}
}

func (fw *FileWatcher) isAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.Contains(AudioExtensions, ext)
}

func (fw *FileWatcher) Close() error {
	return fw.watcher.Close()
}
