package app

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/p-society/raag/internal/domain"
)

func TestLibraryScanner_MultipleProgressHandlers(t *testing.T) {
	sc := NewLibraryScanner(nil, nil, nil, nil, nil)

	var a, b atomic.Int32
	handlerA := func(p ScanProgress) { a.Add(1) }
	handlerB := func(p ScanProgress) { b.Add(1) }

	sc.OnProgress(handlerA)
	sc.OnProgress(handlerB)

	sc.notifyProgress(ScanProgress{Scanned: 1, Total: 2, Phase: domain.ScanPhaseParsing})
	sc.notifyProgress(ScanProgress{Scanned: 2, Total: 2, Phase: domain.ScanPhaseParsing})
	if a.Load() != 2 {
		t.Errorf("handlerA got %d calls, want 2", a.Load())
	}
	if b.Load() != 2 {
		t.Errorf("handlerB got %d calls, want 2", b.Load())
	}
}

func TestLibraryScanner_RemoveProgressHandler(t *testing.T) {
	sc := NewLibraryScanner(nil, nil, nil, nil, nil)

	var a, b atomic.Int32
	handlerA := func(p ScanProgress) { a.Add(1) }
	handlerB := func(p ScanProgress) { b.Add(1) }

	sc.OnProgress(handlerA)
	sc.OnProgress(handlerB)
	sc.RemoveProgressHandler(handlerA)
	sc.notifyProgress(ScanProgress{Scanned: 1, Total: 1, Phase: domain.ScanPhaseParsing})
	if a.Load() != 0 {
		t.Errorf("removed handlerA got %d calls, want 0", a.Load())
	}
	if b.Load() != 1 {
		t.Errorf("remaining handlerB got %d calls, want 1", b.Load())
	}
}

// TestParseFile_SymlinkEscape verifies the scanner refuses to parse a file
// that escapes the configured library root via a symlink (os.Root hardening).
func TestParseFile_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.mp3")
	if err := os.WriteFile(secret, []byte("not audio"), 0o644); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, "escape.mp3")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	sc := NewLibraryScanner(nil, nil, nil, nil, []string{root})
	if _, err := sc.parseFile(link); err == nil {
		t.Fatal("parseFile(symlink escape) succeeded, want error")
	}
}
