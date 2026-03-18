package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/p-society/raag/internal/domain"
)

func TestScanner_NewScanner(t *testing.T) {
	paths := []string{"/music", "/podcasts"}
	scanner := NewScanner(paths)

	if len(scanner.paths) != 2 {
		t.Errorf("NewScanner() paths length = %d, want 2", len(scanner.paths))
	}

	if len(scanner.supportedExts) == 0 {
		t.Error("NewScanner() should initialize supportedExts")
	}

	if scanner.supportedExts[".mp3"] != true {
		t.Error("NewScanner() should support .mp3 extension")
	}

	if scanner.supportedExts[".flac"] != true {
		t.Error("NewScanner() should support .flac extension")
	}
}

func TestScanner_OnProgress(t *testing.T) {
	scanner := NewScanner([]string{})
	called := false
	scanner.OnProgress(func(progress ScanProgress) {
		called = true
	})

	if scanner.onProgress == nil {
		t.Error("OnProgress() should set callback")
	}
	_ = called
}

func TestScanner_OnTrack(t *testing.T) {
	scanner := NewScanner([]string{})

	if scanner.onTrack != nil {
		t.Error("onTrack should be nil initially")
	}

	scanner.OnTrack(func(track *domain.Track) {})

	if scanner.onTrack == nil {
		t.Error("OnTrack() should set callback")
	}
}

func TestScanner_OnError(t *testing.T) {
	scanner := NewScanner([]string{})

	if scanner.onError != nil {
		t.Error("onError should be nil initially")
	}

	scanner.OnError(func(err error) {})

	if scanner.onError == nil {
		t.Error("OnError() should set callback")
	}
}

func TestScanner_Scan_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	defer os.RemoveAll(dir)

	scanner := NewScanner([]string{dir})
	ctx := context.Background()

	files, err := scanner.Scan(ctx)
	if err != nil {
		t.Errorf("Scan() error = %v", err)
	}

	if len(files) != 0 {
		t.Errorf("Scan() found %d files, want 0", len(files))
	}
}

func TestScanner_Scan_WithFiles(t *testing.T) {
	dir := t.TempDir()
	defer os.RemoveAll(dir)

	musicDir := filepath.Join(dir, "music")
	_ = os.MkdirAll(musicDir, 0o755)

	_ = os.WriteFile(filepath.Join(musicDir, "song1.mp3"), []byte("fake mp3"), 0o644)
	_ = os.WriteFile(filepath.Join(musicDir, "song2.flac"), []byte("fake flac"), 0o644)
	_ = os.WriteFile(filepath.Join(musicDir, "readme.txt"), []byte("readme"), 0o644)

	scanner := NewScanner([]string{musicDir})
	ctx := context.Background()

	files, err := scanner.Scan(ctx)
	if err != nil {
		t.Errorf("Scan() error = %v", err)
	}

	if len(files) != 2 {
		t.Errorf("Scan() found %d files, want 2", len(files))
	}
}

func TestScanner_Scan_WithProgress(t *testing.T) {
	dir := t.TempDir()
	defer os.RemoveAll(dir)

	_ = os.WriteFile(filepath.Join(dir, "song.mp3"), []byte("fake"), 0o644)

	progressCalls := 0
	scanner := NewScanner([]string{dir})
	scanner.OnProgress(func(progress ScanProgress) {
		progressCalls++
	})

	ctx := context.Background()
	_, _ = scanner.Scan(ctx)

	if progressCalls == 0 {
		t.Error("OnProgress callback should have been called")
	}
}

func TestScanner_Scan_Cancel(t *testing.T) {
	dir := t.TempDir()
	defer os.RemoveAll(dir)

	_ = os.WriteFile(filepath.Join(dir, "song.mp3"), []byte("fake"), 0o644)

	scanner := NewScanner([]string{dir})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	files, err := scanner.Scan(ctx)
	if err != nil {
		t.Errorf("Scan() error = %v", err)
	}
	if len(files) > 0 {
		t.Errorf("Scan() should return empty files when cancelled, got %d", len(files))
	}
}

func TestScanner_Scan_NestedDirs(t *testing.T) {
	dir := t.TempDir()
	defer os.RemoveAll(dir)

	nested := filepath.Join(dir, "artist", "album")
	_ = os.MkdirAll(nested, 0o755)

	_ = os.WriteFile(filepath.Join(nested, "song.mp3"), []byte("fake"), 0o644)

	scanner := NewScanner([]string{dir})
	ctx := context.Background()

	files, err := scanner.Scan(ctx)
	if err != nil {
		t.Errorf("Scan() error = %v", err)
	}

	if len(files) != 1 {
		t.Errorf("Scan() found %d files, want 1", len(files))
	}
}

func TestScanner_Scan_UnsupportedExt(t *testing.T) {
	dir := t.TempDir()
	defer os.RemoveAll(dir)

	_ = os.WriteFile(filepath.Join(dir, "video.mp4"), []byte("fake"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "image.jpg"), []byte("fake"), 0o644)

	scanner := NewScanner([]string{dir})
	ctx := context.Background()

	files, err := scanner.Scan(ctx)
	if err != nil {
		t.Errorf("Scan() error = %v", err)
	}

	if len(files) != 0 {
		t.Errorf("Scan() should not find video/image files, found %d", len(files))
	}
}

func TestLibraryManager_NewLibraryManager(t *testing.T) {
	scanner := &LibraryScanner{}
	manager := NewLibraryManager(scanner)

	if manager == nil {
		t.Error("NewLibraryManager() should not return nil")
	}

	if manager.scanner != scanner {
		t.Error("NewLibraryManager() should set scanner")
	}
}

func TestScanProgress_Fields(t *testing.T) {
	progress := ScanProgress{
		Phase:       "test",
		TotalFound:  10,
		Processed:   5,
		CurrentFile: "/music/song.mp3",
		Errors:      nil,
	}

	if progress.Phase != "test" {
		t.Errorf("ScanProgress.Phase = %v, want 'test'", progress.Phase)
	}
	if progress.TotalFound != 10 {
		t.Errorf("ScanProgress.TotalFound = %d, want 10", progress.TotalFound)
	}
	if progress.Processed != 5 {
		t.Errorf("ScanProgress.Processed = %d, want 5", progress.Processed)
	}
}
