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

type FileEventHandler interface {
	AddFile(ctx context.Context, path string) error
	RemoveFile(ctx context.Context, path string) error
}

type FileWatcher struct {
	watcher   *fsnotify.Watcher
	paths     []string
	handler   FileEventHandler
	statCache map[string]*domain.FileStat
	mu        sync.Mutex
	pending   map[string]fsnotify.Event
	flushTick *time.Ticker
}

func NewFileWatcher(paths []string, handler FileEventHandler) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	fw := &FileWatcher{
		watcher:   watcher,
		paths:     paths,
		handler:   handler,
		statCache: make(map[string]*domain.FileStat),
		pending:   make(map[string]fsnotify.Event),
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
			_ = watcher.Close()
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
			_ = fw.watcher.Close()
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
			fw.mu.Lock()
			delete(fw.statCache, path)
			fw.mu.Unlock()
			if err := fw.handler.RemoveFile(ctx, path); err != nil {
				slog.Warn("failed to remove file", "path", path, "error", err)
			}
		} else if err == nil && !info.IsDir() {
			fw.mu.Lock()
			prev, exists := fw.statCache[path]
			newStat := &domain.FileStat{
				Path:  path,
				Mtime: info.ModTime().Unix(),
				Size:  info.Size(),
			}

			needsUpdate := !exists || prev.Mtime != newStat.Mtime || prev.Size != newStat.Size
			fw.mu.Unlock()
			if needsUpdate {
				if err := fw.handler.AddFile(ctx, path); err != nil {
					slog.Warn("failed to add file", "path", path, "error", err)
					continue
				}

				fw.mu.Lock()
				fw.statCache[path] = newStat
				fw.mu.Unlock()
			}
		}
	}
}

func (fw *FileWatcher) isAudioFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return slices.Contains(AudioExtensions, ext)
}

func (fw *FileWatcher) Close() error {
	return fw.watcher.Close()
}
