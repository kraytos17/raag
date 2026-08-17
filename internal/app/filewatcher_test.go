package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type recordingHandler struct {
	mu      sync.Mutex
	added   []string
	removed []string
}

func (h *recordingHandler) AddFile(_ context.Context, path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.added = append(h.added, path)
	return nil
}

func (h *recordingHandler) RemoveFile(_ context.Context, path string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removed = append(h.removed, path)
	return nil
}

func (h *recordingHandler) Added() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.added
}

func (h *recordingHandler) Removed() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.removed
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// TestFileWatcherComponent_StartStop verifies the component wraps the watcher
// lifecycle correctly.
func TestFileWatcherComponent_StartStop(t *testing.T) {
	dir := t.TempDir()
	handler := &recordingHandler{}
	fw, err := NewFileWatcher([]string{dir}, handler)
	if err != nil {
		t.Fatalf("NewFileWatcher() error = %v", err)
	}

	comp := NewFileWatcherComponent(fw, "filewatcher")
	if comp.Name() != "filewatcher" {
		t.Fatalf("Name() = %q, want filewatcher", comp.Name())
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := comp.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if err := comp.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	cancel()
}

// TestFileWatcher_AddRemove verifies created audio files trigger AddFile and
// deleted files trigger RemoveFile.
func TestFileWatcher_AddRemove(t *testing.T) {
	dir := t.TempDir()
	handler := &recordingHandler{}
	fw, err := NewFileWatcher([]string{dir}, handler)
	if err != nil {
		t.Fatalf("NewFileWatcher() error = %v", err)
	}
	defer func() { _ = fw.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fw.Start(ctx)

	// Create a new audio file inside the watched dir.
	path := filepath.Join(dir, "new-track.mp3")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if !waitFor(t, 3*time.Second, func() bool { return len(handler.Added()) >= 1 }) {
		t.Fatalf("expected AddFile to be called, added=%v", handler.Added())
	}

	// Remove it and expect RemoveFile.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	if !waitFor(t, 3*time.Second, func() bool { return len(handler.Removed()) >= 1 }) {
		t.Fatalf("expected RemoveFile to be called, removed=%v", handler.Removed())
	}
}

// TestFileWatcher_IgnoresNonAudio verifies non-audio files are not reported.
func TestFileWatcher_IgnoresNonAudio(t *testing.T) {
	dir := t.TempDir()
	handler := &recordingHandler{}
	fw, err := NewFileWatcher([]string{dir}, handler)
	if err != nil {
		t.Fatalf("NewFileWatcher() error = %v", err)
	}
	defer func() { _ = fw.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fw.Start(ctx)

	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	if len(handler.Added()) != 0 {
		t.Fatalf("expected no AddFile for non-audio file, added=%v", handler.Added())
	}
}
